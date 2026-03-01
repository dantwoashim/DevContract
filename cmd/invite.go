package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/envsync/envsync/internal/audit"
	"github.com/envsync/envsync/internal/peer"
	"github.com/envsync/envsync/internal/relay"
	"github.com/spf13/cobra"
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

	// Create invite on relay
	client := relay.NewClient(cfg.Relay.URL, kp)
	err = client.CreateInvite(relay.InviteRequest{
		TokenHash:          tokenHash,
		TeamID:             teamID,
		Inviter:            cfg.Identity.GitHubUsername,
		InviterFingerprint: kp.Fingerprint,
		Invitee:            username,
		ExpectedFingerprint: "", // Will be filled when we fetch GitHub keys
	})
	if err != nil {
		// Relay might not be available — create local invite anyway
		fmt.Printf("  ⚠ Relay unavailable: %s\n", err)
		fmt.Println("    Invite created locally. Peer must be on the same LAN.")
		fmt.Println()
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

// generateMnemonic creates a 6-word random mnemonic token.
func generateMnemonic() string {
	words := []string{
		"tiger", "castle", "moon", "river", "flame", "hope",
		"storm", "eagle", "frost", "blade", "ocean", "crown",
		"spark", "stone", "cloud", "forest", "bridge", "dawn",
		"iron", "coral", "pulse", "ember", "gate", "prism",
		"wind", "orbit", "silk", "dune", "arc", "nova",
		"peak", "wave", "reef", "lens", "mesh", "haze",
	}

	// Generate 6 random indices
	selected := make([]string, 6)
	for i := 0; i < 6; i++ {
		b := make([]byte, 1)
		if _, err := rand.Read(b); err != nil {
			// Fail hard — crypto randomness is critical for invite security
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		selected[i] = words[int(b[0])%len(words)]
	}

	return strings.Join(selected, "-")
}

// generateTeamID creates a deterministic team ID from fingerprint.
func generateTeamID(fingerprint string) string {
	h := sha256.Sum256([]byte(fingerprint + ":default"))
	return fmt.Sprintf("%x", h[:8])
}

func init() {
	rootCmd.AddCommand(inviteCmd)
	rootCmd.AddCommand(joinCmd)
}
