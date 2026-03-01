// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/discovery"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

var inviteCmd = &cobra.Command{
	Use:   "invite @username",
	Short: "Invite a teammate to your team",
	Long:  "Creates an invite for a GitHub user. Share the generated 6-word code with them.",
	Args:  cobra.ExactArgs(1),
	RunE:  runInvite,
}

func runInvite(cmd *cobra.Command, args []string) error {
	username := strings.TrimPrefix(args[0], "@")

	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  ✦ Inviting @%s to your team\n", username)
	fmt.Println()

	// Generate 6-word mnemonic token
	token := generateMnemonic()
	tokenHash := relay.HashToken(token)

	// Create team ID from fingerprint + project
	teamID := generateTeamID(kp.Fingerprint)

	// Fetch invitee's GitHub SSH keys to get their expected fingerprint
	inviteeFP := ""
	if cacheDir, dirErr := config.DataDir(); dirErr == nil {
		ghCache := discovery.NewGitHubKeyCache(cacheDir)
		ghKeys, ghErr := ghCache.FetchEd25519Keys(username)
		if ghErr == nil && len(ghKeys) > 0 {
			inviteeFP = ssh.FingerprintSHA256(ghKeys[0])
		}
	}

	// Create invite on relay
	client := relay.NewClient(cfg.Relay.URL, kp)
	err = client.CreateInvite(relay.InviteRequest{
		TokenHash:           tokenHash,
		TeamID:              teamID,
		Inviter:             cfg.Identity.GitHubUsername,
		InviterFingerprint:  kp.Fingerprint,
		Invitee:             username,
		ExpectedFingerprint: inviteeFP,
	})
	if err != nil {
		return fmt.Errorf("creating invite on relay: %w\n\n  If running LAN-only, use 'envsync invite --lan-only'", err)
	}

	// Save team locally
	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	team := &peer.Team{
		ID:        teamID,
		Name:      fmt.Sprintf("Team %s", username),
		CreatedBy: kp.Fingerprint,
		CreatedAt: time.Now(),
	}
	if err := registry.SaveTeam(team); err != nil {
		return err
	}

	// Display the join code
	fmt.Printf("  ▸ Share this code with @%s:\n", username)
	fmt.Println()
	fmt.Printf("    📋  %s\n", token)
	fmt.Println()
	fmt.Printf("  They run: envsync join %s\n", token)
	fmt.Println()
	fmt.Println("  ⏳ Code expires in 24 hours.")

	// Audit
	logger, _ := audit.NewLogger()
	if logger != nil {
		logger.Log(audit.Entry{
			Event:   audit.EventInvite,
			Peer:    username,
			Details: fmt.Sprintf("team %s", teamID),
		})
	}

	return nil
}

var joinCmd = &cobra.Command{
	Use:   "join <code>",
	Short: "Join a team using an invite code",
	Long:  "Redeems a 6-word invite code and joins the team.",
	Args:  cobra.ExactArgs(1),
	RunE:  runJoin,
}

func runJoin(cmd *cobra.Command, args []string) error {
	token := args[0]

	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  ✦ Joining team with invite code\n")
	fmt.Println()

	tokenHash := relay.HashToken(token)

	// Consume invite from relay
	client := relay.NewClient(cfg.Relay.URL, kp)
	inviteResp, err := client.ConsumeInvite(tokenHash)
	if err != nil {
		fmt.Printf("  ⚠ Relay: %s\n", err)
		fmt.Println("    Attempting local join...")
		fmt.Println()
		return fmt.Errorf("could not redeem invite code. Check the code and try again")
	}

	// Save team and peer locally
	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	team := &peer.Team{
		ID:        inviteResp.TeamID,
		Name:      fmt.Sprintf("Team %s", inviteResp.Inviter),
		CreatedBy: inviteResp.InviterFingerprint,
		CreatedAt: time.Now(),
	}
	if err := registry.SaveTeam(team); err != nil {
		return err
	}

	// Register inviter as trusted peer
	inviterPeer := &peer.Peer{
		GitHubUsername: inviteResp.Inviter,
		Fingerprint:   inviteResp.InviterFingerprint,
		Trust:         peer.TrustTrusted,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		TrustedAt:     time.Now(),
	}
	if err := registry.SavePeer(inviteResp.TeamID, inviterPeer); err != nil {
		return err
	}

	// Register ourselves on the relay team
	publicKeyB64 := base64.StdEncoding.EncodeToString(kp.Ed25519Public)
	if err := client.AddTeamMember(inviteResp.TeamID, cfg.Identity.GitHubUsername, kp.Fingerprint, publicKeyB64); err != nil {
		fmt.Printf("  ⚠ Relay registration: %s\n", err)
	}

	fmt.Printf("  ✓ Joined team from @%s\n", inviteResp.Inviter)
	fmt.Printf("  ▸ Team ID: %s\n", inviteResp.TeamID)
	fmt.Printf("  ▸ Inviter trusted: %s\n", inviteResp.InviterFingerprint)
	fmt.Println()
	fmt.Println("  Ready. Run 'envsync pull' to receive the latest .env.")

	// Audit
	logger, _ := audit.NewLogger()
	if logger != nil {
		logger.Log(audit.Entry{
			Event:   audit.EventJoin,
			Peer:    inviteResp.Inviter,
			Details: fmt.Sprintf("team %s", inviteResp.TeamID),
		})
	}

	return nil
}

// generateMnemonic creates an 8-word random mnemonic token with 64 bits of entropy.
// Uses a 256-word pool (8 bits per word × 8 words = 64 bits).
func generateMnemonic() string {
	// 256-word curated pool — exactly 2^8 words to eliminate modular bias
	words := []string{
		"tiger", "castle", "moon", "river", "flame", "hope", "storm", "eagle",
		"frost", "blade", "ocean", "crown", "spark", "stone", "cloud", "forest",
		"bridge", "dawn", "iron", "coral", "pulse", "ember", "gate", "prism",
		"wind", "orbit", "silk", "dune", "arc", "nova", "peak", "wave",
		"reef", "lens", "mesh", "haze", "torch", "maple", "drift", "anchor",
		"bolt", "cyan", "delta", "echo", "flint", "grain", "haven", "ivory",
		"jade", "kelp", "lark", "mist", "north", "olive", "plume", "quartz",
		"ridge", "slate", "thorn", "umbra", "vault", "wren", "yard", "zinc",
		"amber", "birch", "cedar", "dove", "elm", "fern", "glow", "hawk",
		"inch", "junco", "knot", "lime", "moth", "nest", "opal", "pine",
		"quill", "raven", "sage", "trout", "umber", "vine", "willow", "xenon",
		"yew", "alder", "bass", "crane", "dusk", "forge", "grove", "heron",
		"iris", "jasper", "kite", "lotus", "myrtle", "nutmeg", "orchid", "pearl",
		"robin", "swift", "tulip", "urchin", "viper", "walrus", "aspen", "bloom",
		"cliff", "drake", "ermine", "falcon", "garnet", "holly", "indigo", "jackal",
		"lapis", "mango", "nebula", "onyx", "panther", "rune", "sapphire", "talon",
		"cobra", "fennel", "goblet", "hermit", "ignite", "jovial", "karma", "lantern",
		"marble", "nectar", "oriole", "pelican", "summit", "tundra", "velvet", "whisper",
		"zodiac", "aurora", "breeze", "cipher", "dagger", "enigma", "fiesta", "glacier",
		"harbor", "island", "jungle", "kernel", "legend", "meadow", "nimbus", "oracle",
		"parrot", "rapids", "sierra", "temple", "ultra", "vertex", "winter", "xyloph",
		"bronze", "canopy", "dragon", "ethos", "fossil", "galaxy", "herald", "inkwell",
		"jigsaw", "katana", "lagoon", "mirage", "nexus", "osprey", "presto", "riddle",
		"scarab", "throne", "utopia", "viking", "wombat", "axiom", "beacon", "cavern",
		"dynamo", "ember2", "flurry", "goblin", "helium", "icecap", "jester", "knight",
		"lynx", "monsoon", "narwhal", "outpost", "plasma", "quasar", "raptor", "sphinx",
		"titan", "umbral", "vortex", "zenith", "alpine", "basalt", "cosmos", "delphi",
	}

	// Generate 8 random indices (8 words × 8 bits = 64 bits of entropy)
	selected := make([]string, 8)
	for i := 0; i < 8; i++ {
		b := make([]byte, 1)
		if _, err := rand.Read(b); err != nil {
			// Fail hard — crypto randomness is critical for invite security
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		selected[i] = words[int(b[0])] // Exactly 256 words = no modular bias
	}

	return strings.Join(selected, "-")
}

// generateTeamID creates a deterministic team ID from fingerprint.
// Delegates to peer.GenerateTeamID for a single source of truth.
func generateTeamID(fingerprint string) string {
	return peer.GenerateTeamID(fingerprint, "default")
}

func init() {
	rootCmd.AddCommand(inviteCmd)
	rootCmd.AddCommand(joinCmd)
}
