package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newForwardCommand wraps `bastion-session forward`.
//
// Every other oci-hop command is host-shaped: it ensures auth, a MANAGED_SSH session and an
// SSH host alias for a compute instance. That shape cannot reach things that are not
// compute -- a private OKE API endpoint, a database, an internal service -- because
// MANAGED_SSH needs an OS user and the Bastion plugin on the target. Those need a
// port-forwarding session, which is a different OCI session type.
//
// This stays a thin pass-through on purpose: bastion-session owns session lifecycle,
// reuse, SSH option hardening and the readiness wait, and duplicating any of that here
// would let the two drift.
//
// Requires bastion-session with `forward` support.
func newForwardCommand(waitTimeout *string) *cobra.Command {
	var bastion string
	var privateIP string
	var targetPort int
	var localPort int
	var identityFile string
	var sessionTTL string
	var displayName string
	var format string
	var dryRun bool
	var region string
	var profile string

	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Forward a local port to a private address through OCI Bastion",
		Long: "Open a port-forwarding session to an address inside the bastion's VCN.\n\n" +
			"Use this for targets that are not compute instances -- a private Kubernetes API\n" +
			"endpoint, a database, an internal service. For SSH to a host, use `oci-hop ssh`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(privateIP) == "" {
				return fmt.Errorf("--private-ip is required")
			}
			if targetPort <= 0 {
				return fmt.Errorf("--target-port is required")
			}
			argv := []string{"bastion-session", "forward",
				"--private-ip", privateIP,
				"--target-port", strconv.Itoa(targetPort),
			}
			// Passed through rather than resolved here: bastion-session reads oci-context
			// for scope, and these only override it. Without a region the OCI CLI falls
			// back to its own default and fails with NotAuthorizedOrNotFound, which reads
			// like a permissions problem rather than a wrong-region one.
			if strings.TrimSpace(region) != "" {
				argv = append(argv, "--region", region)
			}
			if strings.TrimSpace(profile) != "" {
				argv = append(argv, "--profile", profile)
			}
			if localPort > 0 {
				argv = append(argv, "--local-port", strconv.Itoa(localPort))
			}
			if strings.TrimSpace(bastion) != "" {
				argv = append(argv, "--bastion-id", bastion)
			}
			if strings.TrimSpace(identityFile) != "" {
				argv = append(argv, "--ssh-private-key", identityFile)
			}
			if strings.TrimSpace(sessionTTL) != "" {
				argv = append(argv, "--session-ttl", sessionTTL)
			}
			if strings.TrimSpace(displayName) != "" {
				argv = append(argv, "--display-name", displayName)
			}
			if waitTimeout != nil && strings.TrimSpace(*waitTimeout) != "" {
				argv = append(argv, "--wait-timeout", *waitTimeout)
			}
			if dryRun {
				argv = append(argv, "--dry-run")
			}
			if f := strings.ToLower(strings.TrimSpace(format)); f == "json" || f == "yaml" || f == "yml" {
				argv = append(argv, "-o", f)
			}
			// Streams stdio: the tunnel is long-lived and Ctrl-C must reach it.
			code, err := runSSHChild(argv)
			if err != nil {
				return err
			}
			if code != 0 {
				return cliError{code: code, msg: "bastion-session forward exited non-zero"}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&bastion, "bastion-id", "", "Bastion OCID (defaults to the selected bastion)")
	cmd.Flags().StringVar(&privateIP, "private-ip", "", "Target private IP inside the bastion's VCN (required)")
	cmd.Flags().IntVar(&targetPort, "target-port", 0, "Target port on the private IP (required)")
	cmd.Flags().IntVar(&localPort, "local-port", 0, "Local port to bind (defaults to --target-port)")
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file to pass to bastion-session")
	cmd.Flags().StringVar(&sessionTTL, "session-ttl", "", "Session TTL as a duration or seconds (e.g. 3h, 10800)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Session display name")
	cmd.Flags().StringVarP(&region, "region", "r", "", "OCI region identifier (overrides oci-context)")
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "OCI profile name (overrides oci-context)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Create the session and print the ssh command without connecting")
	cmd.Flags().StringVarP(&format, "output", "o", "auto", "output format: auto, json, or text")
	_ = cmd.RegisterFlagCompletionFunc("identity-file", fileCompletion)
	_ = cmd.RegisterFlagCompletionFunc("output", autoOutputFormatCompletion)
	return cmd
}
