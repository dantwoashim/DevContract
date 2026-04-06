// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/config"
	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/spf13/cobra"
)

var inviteCmd = &cobra.Command{
	Use:   "invite <label>",
	Short: "Invite another member to your project",
	Long:  "Creates an expiring invite code for this project's stable ID. The label is display metadata only.",
	Args:  cobra.ExactArgs(1),
	RunE:  runInvite,
}

func runInvite(cmd *cobra.Command, args []string) error {
	memberLabel := strings.TrimPrefix(args[0], "@")

	kp, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	project, err := ensureProjectContext(cfg)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  * Inviting %s to project %s\n", memberLabel, project.ProjectID)
	fmt.Println()

	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	team := defaultTeam(project.Config.Name, project.ProjectID, kp.Fingerprint)
	if err := registry.SaveTeam(team); err != nil {
		return err
	}

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	if err := client.AddTeamMember(
		project.ProjectID,
		displayMemberLabel(cfg, kp),
		kp.Fingerprint,
		ed25519PublicKeyBase64(kp),
		x25519PublicKeyBase64(kp),
		crypto.ComputeFingerprint(kp.X25519Public),
		"owner",
	); err != nil {
		return fmt.Errorf("registering project owner on relay: %w", err)
	}

	token, err := generateMnemonic()
	if err != nil {
		return fmt.Errorf("generating invite code: %w", err)
	}
	tokenHash := relay.HashToken(token)

	err = client.CreateInvite(relay.InviteRequest{
		TokenHash:          tokenHash,
		TeamID:             project.ProjectID,
		Inviter:            displayMemberLabel(cfg, kp),
		InviterFingerprint: kp.Fingerprint,
		Invitee:            memberLabel,
	})
	if err != nil {
		return fmt.Errorf("creating invite on relay: %w", err)
	}

	fmt.Printf("  - Share this code with %s:\n", memberLabel)
	fmt.Println()
	fmt.Printf("    %s\n", token)
	fmt.Println()
	fmt.Printf("  They run: envsync join %s\n", token)
	fmt.Println()
	fmt.Println("  Code expires in 24 hours.")

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventInvite,
			Peer:    memberLabel,
			Details: fmt.Sprintf("project %s", project.ProjectID),
		})
	}

	return nil
}

var joinCmd = &cobra.Command{
	Use:   "join <code>",
	Short: "Join a project using an invite code",
	Long:  "Redeems an invite code, registers this device, and stores the trusted peer transport keys locally.",
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

	project, err := ensureProjectContext(cfg)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("  * Joining project with invite code")
	fmt.Println()

	client := relay.NewClient(projectRelayURL(project, cfg), kp)
	inviteResp, err := client.ConsumeInvite(relay.HashToken(token), displayMemberLabel(cfg, kp))
	if err != nil {
		return fmt.Errorf("could not redeem invite code: %w", err)
	}

	if project.ProjectID != "" && project.ProjectID != inviteResp.TeamID {
		project.Config.ProjectID = inviteResp.TeamID
		project.ProjectID = inviteResp.TeamID
	}
	if err := peer.SaveProjectConfig(project.Config); err != nil {
		return err
	}

	registry, err := peer.NewRegistry()
	if err != nil {
		return err
	}

	team := defaultTeam(inviteResp.TeamID, inviteResp.TeamID, inviteResp.InviterFingerprint)
	if project.Config.Name != "" {
		team.Name = project.Config.Name
	}
	if err := registry.SaveTeam(team); err != nil {
		return err
	}

	if err := client.AddTeamMember(
		inviteResp.TeamID,
		displayMemberLabel(cfg, kp),
		kp.Fingerprint,
		ed25519PublicKeyBase64(kp),
		x25519PublicKeyBase64(kp),
		crypto.ComputeFingerprint(kp.X25519Public),
		"member",
	); err != nil {
		return fmt.Errorf("registering device on relay: %w", err)
	}

	members, err := client.ListTeamMembers(inviteResp.TeamID)
	if err != nil {
		return fmt.Errorf("fetching project members: %w", err)
	}

	for _, member := range members {
		if member.Fingerprint == kp.Fingerprint {
			continue
		}

		trustState := peer.TrustPending
		trustedAt := time.Time{}
		if member.Fingerprint == inviteResp.InviterFingerprint {
			trustState = peer.TrustTrusted
			trustedAt = time.Now()
		}

		p := &peer.Peer{
			DisplayName:          member.Username,
			RelayUsername:        member.Username,
			Fingerprint:          member.Fingerprint,
			Ed25519Public:        member.PublicKey,
			X25519Public:         member.TransportPublicKey,
			TransportFingerprint: member.TransportFingerprint,
			Trust:                trustState,
			FirstSeen:            time.Now(),
			LastSeen:             time.Now(),
			TrustedAt:            trustedAt,
		}
		if err := registry.SavePeer(inviteResp.TeamID, p); err != nil {
			return err
		}
		team.AddMember(member.Fingerprint)
	}
	team.AddMember(kp.Fingerprint)
	if err := registry.SaveTeam(team); err != nil {
		return err
	}

	fmt.Printf("  + Joined project %s from %s\n", inviteResp.TeamID, inviteResp.Inviter)
	fmt.Printf("  - Trusted identity: %s\n", inviteResp.InviterFingerprint)
	fmt.Println("  - Other relay members were imported as pending until you verify them locally.")
	fmt.Println()
	fmt.Println("  Ready. Run 'envsync pull' to receive the latest .env.")

	logger, _ := audit.NewLogger()
	if logger != nil {
		_ = logger.Log(audit.Entry{
			Event:   audit.EventJoin,
			Peer:    inviteResp.Inviter,
			Details: fmt.Sprintf("project %s", inviteResp.TeamID),
		})
	}

	return nil
}

// generateMnemonic creates an 8-word random mnemonic token with 64 bits of entropy.
func generateMnemonic() (string, error) {
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

	selected := make([]string, 8)
	for i := 0; i < 8; i++ {
		b := make([]byte, 2)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("crypto/rand failed: %w", err)
		}
		idx := int(b[0])<<8 | int(b[1])
		selected[i] = words[idx%len(words)]
	}

	return strings.Join(selected, "-"), nil
}

func projectRelayURL(project *projectContext, cfg *config.Config) string {
	if project != nil && project.Config != nil && project.Config.RelayURL != "" {
		return project.Config.RelayURL
	}
	if cfg != nil && cfg.Relay.URL != "" {
		return cfg.Relay.URL
	}
	return "https://relay.envsync.dev"
}

func init() {
	rootCmd.AddCommand(inviteCmd)
	rootCmd.AddCommand(joinCmd)
}
