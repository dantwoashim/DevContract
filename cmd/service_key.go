// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"encoding/base64"
	"fmt"

	"github.com/envsync/envsync/internal/crypto"
	"github.com/envsync/envsync/internal/relay"
	"github.com/envsync/envsync/internal/ui"
	"github.com/spf13/cobra"
)

var serviceKeyCmd = &cobra.Command{
	Use:   "service-key",
	Short: "Manage service account keys for CI/CD",
	Long:  "Generate, export, and register service keys for use in GitHub Actions and other CI environments.",
}

var serviceKeyGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new service account key",
	RunE:  runServiceKeyGenerate,
}

var serviceKeyExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the public key for team registration",
	RunE:  runServiceKeyExport,
}

var serviceKeyRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a service key with the current project",
	RunE:  runServiceKeyRegister,
}

var (
	serviceKeyOutput       string
	serviceKeyRegisterTeam string
	serviceKeyRegisterName string
)

func runServiceKeyGenerate(cmd *cobra.Command, args []string) error {
	sk, err := crypto.GenerateServiceKey()
	if err != nil {
		return err
	}

	outPath := serviceKeyOutput
	if outPath == "" {
		outPath = ".envsync-service-key"
	}

	if err := sk.SaveToFile(outPath); err != nil {
		return err
	}

	ui.Header("Service Key Generated")
	ui.Success(fmt.Sprintf("Private key saved to: %s", outPath))
	ui.Blank()
	ui.Line("Add this to your GitHub Actions secrets:")
	ui.Blank()
	ui.Code(fmt.Sprintf("  ENVSYNC_SERVICE_KEY=%s", base64.StdEncoding.EncodeToString(sk.ExportPrivateKey())))
	ui.Blank()
	ui.Line("Public key (for team registration):")
	ui.Code(fmt.Sprintf("  %s", base64.StdEncoding.EncodeToString(sk.PublicKey)))
	ui.Blank()
	ui.Warning("Keep the private key secret. Never commit it to git.")
	ui.Line("Add '.envsync-service-key' to your .gitignore.")

	return nil
}

func runServiceKeyExport(cmd *cobra.Command, args []string) error {
	keyPath := serviceKeyOutput
	if keyPath == "" {
		keyPath = ".envsync-service-key"
	}

	sk, err := crypto.LoadServiceKeyFromFile(keyPath)
	if err != nil {
		ui.RenderError(ui.StructuredError{
			Category:   ui.ErrFile,
			Message:    "Service key not found",
			Cause:      fmt.Sprintf("Expected key at %s", keyPath),
			Suggestion: "Run 'envsync service-key generate' first",
		})
		return err
	}

	pubKeyB64 := base64.StdEncoding.EncodeToString(sk.PublicKey)
	fmt.Println(pubKeyB64)

	return nil
}

func runServiceKeyRegister(cmd *cobra.Command, args []string) error {
	ownerKP, err := loadIdentity()
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	project, err := requireProjectContext()
	if err != nil {
		return err
	}

	keyPath := serviceKeyOutput
	if keyPath == "" {
		keyPath = ".envsync-service-key"
	}

	serviceKP, err := loadIdentityFromServiceKey(keyPath)
	if err != nil {
		return err
	}

	targetProjectID := project.ProjectID
	if serviceKeyRegisterTeam != "" {
		targetProjectID = serviceKeyRegisterTeam
	}

	memberName := serviceKeyRegisterName
	if memberName == "" {
		memberName = "ci"
	}

	client := relay.NewClient(projectRelayURL(project, cfg), ownerKP)
	if err := client.AddTeamMember(
		targetProjectID,
		memberName,
		serviceKP.Fingerprint,
		ed25519PublicKeyBase64(serviceKP),
		x25519PublicKeyBase64(serviceKP),
		"member",
	); err != nil {
		return fmt.Errorf("registering service key: %w", err)
	}

	ui.Header("Service Key Registered")
	ui.Success(fmt.Sprintf("Registered %s on project %s", memberName, targetProjectID))
	ui.Blank()
	ui.Line("This key can now authenticate relay pulls in CI with:")
	ui.Code(fmt.Sprintf("  envsync pull --service-key %s --team %s", keyPath, targetProjectID))
	ui.Blank()

	return nil
}

func init() {
	serviceKeyGenerateCmd.Flags().StringVarP(&serviceKeyOutput, "output", "o", "", "Output path for key file")
	serviceKeyExportCmd.Flags().StringVarP(&serviceKeyOutput, "key", "k", "", "Path to key file")
	serviceKeyRegisterCmd.Flags().StringVarP(&serviceKeyOutput, "key", "k", "", "Path to key file")
	serviceKeyRegisterCmd.Flags().StringVar(&serviceKeyRegisterTeam, "team", "", "Override the current project ID")
	serviceKeyRegisterCmd.Flags().StringVar(&serviceKeyRegisterName, "name", "ci", "Member name to register for the service key")

	serviceKeyCmd.AddCommand(serviceKeyGenerateCmd)
	serviceKeyCmd.AddCommand(serviceKeyExportCmd)
	serviceKeyCmd.AddCommand(serviceKeyRegisterCmd)
	rootCmd.AddCommand(serviceKeyCmd)
}
