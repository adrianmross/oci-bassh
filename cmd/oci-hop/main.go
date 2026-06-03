package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	primaryCommand   = "hop"
	qualifiedCommand = "oci-hop"
	defaultWaitTime  = "2m"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type commandResult struct {
	Command     []string         `json:"command"`
	OK          bool             `json:"ok"`
	ExitCode    int              `json:"exit_code"`
	Stdout      string           `json:"stdout"`
	Stderr      string           `json:"stderr"`
	ErrorCode   string           `json:"error_code,omitempty"`
	Message     string           `json:"message,omitempty"`
	NextCommand string           `json:"next_command,omitempty"`
	JSON        *json.RawMessage `json:"json,omitempty"`
}

type issue struct {
	ErrorCode   string `json:"error_code"`
	Message     string `json:"message"`
	NextCommand string `json:"next_command,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}

func run(args []string) error {
	if host, identityFile, output, waitTimeout, handled, err := parseRootHopArgs(args); handled || err != nil {
		if err != nil {
			return err
		}
		return cmdHop(host, identityFile, output, waitTimeout)
	}
	root := newRootCommand(os.Stdout, os.Stderr)
	root.SetArgs(args)
	return root.Execute()
}

func parseRootHopArgs(args []string) (host, identityFile, output, waitTimeout string, handled bool, err error) {
	output = "text"
	waitTimeout = defaultWaitTime
	remaining := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			remaining = append(remaining, args[i+1:]...)
			i = len(args)
		case arg == "-h" || arg == "--help" || arg == "--version" || arg == "--json" || arg == "-v" || arg == "--verbose-version":
			return "", "", "", "", false, nil
		case arg == "-o" || arg == "--output":
			i++
			if i >= len(args) {
				return "", "", "", "", true, cliError{code: 2, msg: arg + " requires a value"}
			}
			output = args[i]
		case strings.HasPrefix(arg, "--output="):
			output = strings.TrimPrefix(arg, "--output=")
		case arg == "--identity-file":
			i++
			if i >= len(args) {
				return "", "", "", "", true, cliError{code: 2, msg: "--identity-file requires a value"}
			}
			identityFile = args[i]
		case strings.HasPrefix(arg, "--identity-file="):
			identityFile = strings.TrimPrefix(arg, "--identity-file=")
		case arg == "--wait-timeout":
			i++
			if i >= len(args) {
				return "", "", "", "", true, cliError{code: 2, msg: "--wait-timeout requires a value"}
			}
			waitTimeout = args[i]
		case strings.HasPrefix(arg, "--wait-timeout="):
			waitTimeout = strings.TrimPrefix(arg, "--wait-timeout=")
		case strings.HasPrefix(arg, "-"):
			return "", "", "", "", false, nil
		default:
			remaining = append(remaining, arg)
		}
	}
	if len(remaining) == 0 {
		return "", "", "", "", false, nil
	}
	if knownRootCommand(remaining[0]) {
		return "", "", "", "", false, nil
	}
	if len(remaining) > 1 {
		return "", "", "", "", true, cliError{code: 2, msg: "unknown command or too many arguments: " + strings.Join(remaining, " ")}
	}
	return remaining[0], identityFile, output, waitTimeout, true, nil
}

func knownRootCommand(name string) bool {
	switch name {
	case "doctor", "check", "inspect", "repair", "ensure", "ensure-target", "track", "track-from-terraform", "ssh", "explain", "paths", "setup", "upgrade", "version", "completion", "contract-check", "help":
		return true
	default:
		return false
	}
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	var rootVersion bool
	var rootJSON bool
	var versionCount int
	var rootOutput string
	var rootIdentityFile string
	var rootWaitTimeout string
	root := &cobra.Command{
		Use:           commandName(),
		Short:         "Prepare SSH access to OCI compute hosts through OCI Bastion",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootVersion || rootJSON || versionCount > 0 {
				format := "text"
				if rootJSON {
					format = "json"
				}
				return emitVersion(format, versionCount > 1)
			}
			if len(args) == 1 {
				return cmdHop(args[0], rootIdentityFile, rootOutput, rootWaitTimeout)
			}
			if len(args) > 1 {
				return cliError{code: 2, msg: "unknown command or too many arguments: " + strings.Join(args, " ")}
			}
			return cmd.Help()
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.Flags().BoolVar(&rootVersion, "version", false, "print version and exit")
	root.Flags().BoolVar(&rootJSON, "json", false, "with --version, print JSON version details")
	root.Flags().CountVarP(&versionCount, "verbose-version", "v", "print version; repeat for commit and date")
	root.Flags().StringVarP(&rootOutput, "output", "o", "text", "output format for <host>: text or json")
	root.Flags().StringVar(&rootIdentityFile, "identity-file", "", "SSH identity file to pass to bastion-session for <host>")
	root.PersistentFlags().StringVar(&rootWaitTimeout, "wait-timeout", defaultWaitTime, "how long to wait for a new Bastion session to become ACTIVE")
	_ = root.RegisterFlagCompletionFunc("output", outputFormatCompletion)
	_ = root.RegisterFlagCompletionFunc("identity-file", fileCompletion)

	root.AddCommand(
		newDoctorCommand(),
		newCheckCommand(),
		newInspectCommand(),
		newRepairCommand(),
		newEnsureCommand("ensure", &rootWaitTimeout),
		newEnsureCommand("ensure-target", &rootWaitTimeout),
		newTrackCommand("track"),
		newTrackCommand("track-from-terraform"),
		newSSHCommand(),
		newExplainCommand(),
		newPathsCommand(),
		newSetupCommand(),
		newUpgradeCommand(),
		newVersionCommand(),
		newCompletionCommand(root),
		newContractCheckCommand(),
	)
	return root
}

func commandName() string {
	name := filepath.Base(os.Args[0])
	switch name {
	case primaryCommand, qualifiedCommand:
		return name
	default:
		return primaryCommand
	}
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "doctor [host]",
		Short:             "Run tolerant OCI Bastion diagnostics",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdDoctor(args)
		},
	}
}

func newCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "check [host]",
		Short:             "Run strict OCI Bastion health checks",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdCheck(args)
		},
	}
}

func newInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "inspect <host>",
		Short:             "Inspect cached OCI, Bastion, and SSH state for a host",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdInspect(args)
		},
	}
}

func newRepairCommand() *cobra.Command {
	var ensure bool
	var identityFile string
	cmd := &cobra.Command{
		Use:               "repair <host>",
		Short:             "Repair Bastion SSH setup for a host",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			legacyArgs := []string{}
			if ensure {
				legacyArgs = append(legacyArgs, "--ensure")
			}
			if identityFile != "" {
				legacyArgs = append(legacyArgs, "--identity-file", identityFile)
			}
			legacyArgs = append(legacyArgs, args[0])
			return cmdRepair(legacyArgs)
		},
	}
	cmd.Flags().BoolVar(&ensure, "ensure", false, "also ensure auth, Bastion session, and SSH config")
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file to pass to bastion-session")
	_ = cmd.RegisterFlagCompletionFunc("identity-file", fileCompletion)
	return cmd
}

func newEnsureCommand(name string, waitTimeout *string) *cobra.Command {
	var identityFile string
	cmd := &cobra.Command{
		Use:               name + " <host>",
		Short:             "Ensure auth, Bastion session, and SSH config for a host",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			legacyArgs := []string{}
			if identityFile != "" {
				legacyArgs = append(legacyArgs, "--identity-file", identityFile)
			}
			if waitTimeout != nil && *waitTimeout != "" {
				legacyArgs = append(legacyArgs, "--wait-timeout", *waitTimeout)
			}
			legacyArgs = append(legacyArgs, args[0])
			return cmdEnsure(legacyArgs)
		},
	}
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file to pass to bastion-session")
	_ = cmd.RegisterFlagCompletionFunc("identity-file", fileCompletion)
	return cmd
}

func newTrackCommand(name string) *cobra.Command {
	var terraformDir string
	var user string
	var identityFile string
	cmd := &cobra.Command{
		Use:               name + " <host> [terraform-outputs]",
		Short:             "Track a host from Terraform outputs",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: pathAfterHostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			legacyArgs := []string{args[0]}
			if terraformDir != "" {
				legacyArgs = append(legacyArgs, "--terraform-dir", terraformDir)
			} else if len(args) == 2 {
				legacyArgs = append(legacyArgs, args[1])
			}
			if user != "" {
				legacyArgs = append(legacyArgs, "--user", user)
			}
			if identityFile != "" {
				legacyArgs = append(legacyArgs, "--identity-file", identityFile)
			}
			return cmdTrack(legacyArgs)
		},
	}
	cmd.Flags().StringVar(&terraformDir, "terraform-dir", "", "Terraform directory or outputs path")
	cmd.Flags().StringVar(&user, "user", "", "SSH user to pass to bastion-session")
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file to pass to bastion-session")
	_ = cmd.RegisterFlagCompletionFunc("terraform-dir", dirCompletion)
	_ = cmd.RegisterFlagCompletionFunc("identity-file", fileCompletion)
	return cmd
}

func newSSHCommand() *cobra.Command {
	var dryRun bool
	var identityFile string
	cmd := &cobra.Command{
		Use:                   "ssh [--dry-run] [--identity-file PATH] <host> [-- ssh args...]",
		Short:                 "Ensure setup and connect with ssh",
		ValidArgsFunction:     hostCompletion,
		DisableFlagParsing:    true,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cmd.Help()
			}
			return cmdSSH(args)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "emit JSON with the ssh command instead of connecting")
	cmd.Flags().StringVar(&identityFile, "identity-file", "", "SSH identity file to pass to bastion-session")
	_ = cmd.RegisterFlagCompletionFunc("identity-file", fileCompletion)
	return cmd
}

func newExplainCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "explain <host>",
		Short:             "Explain the current OCI Bastion SSH path for a host",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hostCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdExplain(args)
		},
	}
}

func newPathsCommand() *cobra.Command {
	format := "text"
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Print local paths used by OCI Bastion Hopper",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdPaths(format)
		},
	}
	cmd.Flags().StringVarP(&format, "output", "o", "text", "output format: json or text")
	_ = cmd.RegisterFlagCompletionFunc("output", outputFormatCompletion)
	return cmd
}

func newSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install local shell integration",
	}
	cmd.AddCommand(newSetupShellCommand())
	return cmd
}

func newSetupShellCommand() *cobra.Command {
	var install bool
	var shellName string
	var outPath string
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Print or install shell integration for hop",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdSetupShell(shellName, outPath, install)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "write the shell integration file")
	cmd.Flags().StringVar(&shellName, "shell", "zsh", "shell to configure: zsh")
	cmd.Flags().StringVar(&outPath, "out", "", "output path for --install")
	return cmd
}

func newUpgradeCommand() *cobra.Command {
	var runInstaller bool
	var prefix string
	var releaseVersion string
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Print or run safe installer guidance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdUpgrade(runInstaller, prefix, releaseVersion)
		},
	}
	cmd.Flags().BoolVar(&runInstaller, "run", false, "run the installer command; default is dry-run guidance")
	cmd.Flags().StringVar(&prefix, "prefix", "", "installation prefix to pass as PREFIX")
	cmd.Flags().StringVar(&releaseVersion, "release", "", "release version to pass as VERSION")
	return cmd
}

func newVersionCommand() *cobra.Command {
	format := "text"
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print OCI Bastion Hopper version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				format = "json"
			}
			return emitVersion(format, false)
		},
	}
	cmd.Flags().StringVarP(&format, "output", "o", "text", "output format: json or text")
	cmd.Flags().Bool("json", false, "print JSON version details")
	_ = cmd.RegisterFlagCompletionFunc("output", outputFormatCompletion)
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion bash|zsh|fish|powershell",
		Short:     "Generate shell completion scripts",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletion(os.Stdout)
			default:
				return cliError{code: 2, msg: "completion requires bash, zsh, fish, or powershell"}
			}
		},
	}
}

func newContractCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "contract-check",
		Short: "Verify downstream JSON command contracts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdContractCheck(args)
		},
	}
}

func cmdDoctor(args []string) error {
	host, err := optionalHost(args)
	if err != nil {
		return err
	}
	out := doctorPayload(host)
	return emit(out)
}

func cmdCheck(args []string) error {
	host, err := optionalHostFor("check", args)
	if err != nil {
		return err
	}
	out := doctorPayload(host)
	if err := emit(out); err != nil {
		return err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return cliError{code: 1, msg: "check found issues"}
	}
	return nil
}

func cmdHop(host, identityFile, format, waitTimeout string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return cliError{code: 2, msg: "host is required"}
	}
	if format != "" && format != "text" && format != "json" {
		return cliError{code: 2, msg: "output must be json or text"}
	}
	hopProgress(format, "checking OCI auth...")
	auth := runJSON("oci-context", "auth", "ensure", "--output", "json")
	ensureArgs := []string{"ensure", host, "-o", "json"}
	if identityFile != "" {
		ensureArgs = append(ensureArgs, "--identity-file", identityFile)
	}
	if waitTimeout != "" {
		ensureArgs = append(ensureArgs, "--wait-timeout", waitTimeout)
	}
	hopProgress(format, "ensuring Bastion session for %s (timeout %s)...", host, effectiveWaitTimeout(waitTimeout))
	ensured := runJSON("bastion-session", ensureArgs...)
	hopProgress(format, "refreshing SSH config for %s...", host)
	sshConfig := runJSON("bastion-session", "ssh-config", "show", host, "-o", "json")
	ok := auth.OK && ensured.OK && sshConfig.OK
	out := map[string]any{
		"ok":         ok,
		"host":       host,
		"auth":       auth,
		"ensure":     ensured,
		"ssh_config": sshConfig,
	}
	if !ok {
		out["issue"] = firstIssue("hop failed", nextForHost(commandName()+" repair --ensure", host), auth, ensured, sshConfig)
	}
	switch format {
	case "json":
		if err := emit(out); err != nil {
			return err
		}
	case "text", "":
		if ok {
			fmt.Fprintln(os.Stdout, compactReadyLine(host, ensured, sshConfig))
		} else if err := emit(out); err != nil {
			return err
		}
	}
	if !ok {
		return cliError{code: 1, msg: "hop failed"}
	}
	return nil
}

func hopProgress(format, message string, args ...any) {
	if format != "" && format != "text" {
		return
	}
	fmt.Fprintf(os.Stderr, message+"\n", args...)
}

func effectiveWaitTimeout(waitTimeout string) string {
	if strings.TrimSpace(waitTimeout) == "" {
		return "default"
	}
	return waitTimeout
}

func doctorPayload(host string) map[string]any {
	tools := map[string]string{}
	for _, name := range []string{"oci-context", "bastion-session", "ssh"} {
		if p, err := exec.LookPath(name); err == nil {
			tools[name] = p
		}
	}
	oci := runJSON("oci-context", "doctor", "-o", "json")
	var bastion commandResult
	if host == "" {
		bastion = runJSON("bastion-session", "doctor", "-o", "json")
	} else {
		bastion = runJSON("bastion-session", "doctor", host, "-o", "json")
	}
	targets := runJSON("bastion-session", "target", "list", "-o", "json")
	ok := oci.OK && bastion.OK && targets.OK && tools["oci-context"] != "" && tools["bastion-session"] != "" && tools["ssh"] != ""
	out := map[string]any{
		"ok":             ok,
		"host":           host,
		"tools":          tools,
		"versions":       versions(),
		"oci_context":    oci,
		"bastion_doctor": bastion,
		"targets":        targets,
	}
	if !ok {
		out["issue"] = firstIssue("doctor found issues", nextForHost(commandName()+" repair", host), oci, bastion, targets)
	}
	return out
}

func cmdInspect(args []string) error {
	host, err := requiredHostOnly("inspect", args)
	if err != nil {
		return err
	}
	status := runJSON("oci-context", "status", "--cached", "-o", "json")
	auth := runJSON("oci-context", "auth", "show", "--output", "json")
	bastion := runJSON("bastion-session", "doctor", host, "--cached", "-o", "json")
	sshConfig := runJSON("bastion-session", "ssh-config", "show", host, "-o", "json")
	sshEffective := runCommand("ssh", "-G", host)
	ok := status.OK && auth.OK && bastion.OK && sshConfig.OK && sshEffective.OK
	out := map[string]any{
		"ok":             ok,
		"host":           host,
		"versions":       versions(),
		"oci_status":     status,
		"auth":           auth,
		"bastion_doctor": bastion,
		"ssh_config":     sshConfig,
		"ssh_effective":  sshEffective,
	}
	if !ok {
		out["issue"] = firstIssue("inspect found issues", nextForHost(commandName()+" repair", host), status, auth, bastion, sshConfig, sshEffective)
	}
	return emit(out)
}

func cmdRepair(args []string) error {
	host, identityFile, ensure, err := parseRepairArgs(args)
	if err != nil {
		return err
	}
	repaired := runJSON("bastion-session", "doctor", host, "--fix", "-o", "json")
	var auth commandResult
	var ensured commandResult
	var sshConfig commandResult
	connectCommand := "ssh " + host
	ok := repaired.OK
	if ensure {
		auth = runJSON("oci-context", "auth", "ensure", "--output", "json")
		ensureArgs := []string{"ensure", host, "-o", "json"}
		if identityFile != "" {
			ensureArgs = append(ensureArgs, "--identity-file", identityFile)
		}
		ensured = runJSON("bastion-session", ensureArgs...)
		sshConfig = runJSON("bastion-session", "ssh-config", "show", host, "-o", "json")
		ok = auth.OK && ensured.OK && sshConfig.OK
		connectCommand = connectCommandFrom(host, ensured)
	}
	out := map[string]any{
		"ok":               ok,
		"host":             host,
		"repair":           repaired,
		"ensure_requested": ensure,
		"connect_command":  connectCommand,
	}
	if ensure {
		out["auth"] = auth
		out["ensure"] = ensured
		out["ssh_config"] = sshConfig
		if !repaired.OK {
			out["repair_issue"] = firstIssue("repair found remaining issues", nextForHost(commandName()+" inspect", host), repaired)
		}
	}
	if !ok {
		out["issue"] = firstIssue("repair failed", nextForHost(commandName()+" inspect", host), auth, ensured, sshConfig, repaired)
	}
	if err := emit(out); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "repair failed"}
	}
	return nil
}

func cmdEnsure(args []string) error {
	host, identityFile, waitTimeout, err := parseHostIdentityWait(args)
	if err != nil {
		return err
	}
	auth := runJSON("oci-context", "auth", "ensure", "--output", "json")
	ensureArgs := []string{"ensure", host, "-o", "json"}
	if identityFile != "" {
		ensureArgs = append(ensureArgs, "--identity-file", identityFile)
	}
	if waitTimeout != "" {
		ensureArgs = append(ensureArgs, "--wait-timeout", waitTimeout)
	}
	ensured := runJSON("bastion-session", ensureArgs...)
	sshConfig := runJSON("bastion-session", "ssh-config", "show", host, "-o", "json")
	ok := auth.OK && ensured.OK && sshConfig.OK
	connectCommand := connectCommandFrom(host, ensured)
	out := map[string]any{
		"ok":              ok,
		"host":            host,
		"auth":            auth,
		"ensure":          ensured,
		"ssh_config":      sshConfig,
		"connect_command": connectCommand,
	}
	if !ok {
		out["issue"] = firstIssue("ensure failed", nextForHost(commandName()+" repair --ensure", host), auth, ensured, sshConfig)
	}
	if err := emit(out); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "ensure failed"}
	}
	return nil
}

func cmdTrack(args []string) error {
	host, tf, passthrough, err := parseTrackArgs(args)
	if err != nil {
		return err
	}
	cmdArgs := []string{"target", "import", host, "--terraform-outputs", tf}
	cmdArgs = append(cmdArgs, passthrough...)
	tracked := runJSON("bastion-session", cmdArgs...)
	shown := runJSON("bastion-session", "target", "show", host, "-o", "json")
	ok := tracked.OK && shown.OK
	out := map[string]any{"ok": ok, "host": host, "track": tracked, "target": shown}
	if !ok {
		out["issue"] = firstIssue("track failed", nextForHost(commandName()+" inspect", host), tracked, shown)
	}
	if err := emit(out); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "track failed"}
	}
	return nil
}

func cmdSSH(args []string) error {
	host, identityFile, dryRun, sshArgs, err := parseSSHArgs(args)
	if err != nil {
		return err
	}
	auth := runJSON("oci-context", "auth", "ensure", "--output", "json")
	ensureArgs := []string{"ensure", host, "-o", "json"}
	if identityFile != "" {
		ensureArgs = append(ensureArgs, "--identity-file", identityFile)
	}
	ensured := runJSON("bastion-session", ensureArgs...)
	ok := auth.OK && ensured.OK
	sshCmd := append([]string{"ssh", host}, sshArgs...)
	if dryRun {
		out := map[string]any{"ok": ok, "host": host, "auth": auth, "ensure": ensured, "ssh_command": sshCmd}
		if !ok {
			out["issue"] = firstIssue("ssh preparation failed", nextForHost(commandName()+" repair --ensure", host), auth, ensured)
		}
		if err := emit(out); err != nil {
			return err
		}
		if !ok {
			return cliError{code: 1, msg: "ssh preparation failed"}
		}
		return nil
	}
	if !ok {
		out := map[string]any{"ok": false, "host": host, "auth": auth, "ensure": ensured, "ssh_command": sshCmd, "issue": firstIssue("ssh preparation failed", nextForHost(commandName()+" repair --ensure", host), auth, ensured)}
		if err := emit(out); err != nil {
			return err
		}
		return cliError{code: 1, msg: "ssh preparation failed"}
	}
	return syscallExec("ssh", sshCmd)
}

func cmdExplain(args []string) error {
	host, err := requiredHostOnly("explain", args)
	if err != nil {
		return err
	}
	explained := runJSON("bastion-session", "explain", host, "-o", "json")
	ok := explained.OK
	out := map[string]any{"ok": ok, "host": host, "explain": explained}
	if !ok {
		out["issue"] = firstIssue("explain failed", nextForHost(commandName()+" inspect", host), explained)
	}
	if err := emit(out); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "explain failed"}
	}
	return nil
}

func cmdPaths(format string) error {
	payload := pathsPayload()
	switch format {
	case "json":
		return emit(payload)
	case "text", "":
		for _, key := range []string{"executable", "hop_binary", "qualified_binary", "home", "oci_context_config", "oci_config", "ssh_config", "ssh_dir", "bastion_cache", "install_script"} {
			fmt.Fprintf(os.Stdout, "%s=%s\n", key, payload["paths"].(map[string]string)[key])
		}
		return nil
	default:
		return cliError{code: 2, msg: "paths output must be json or text"}
	}
}

func cmdSetupShell(shellName, outPath string, install bool) error {
	if shellName != "zsh" {
		return cliError{code: 2, msg: "only zsh setup is currently supported"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath = filepath.Join(home, ".zshrc.d", "oci-hop.zsh")
	}
	snippet := shellIntegrationSnippet()
	if !install {
		fmt.Fprint(os.Stdout, snippet)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(snippet), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Wrote %s\n", outPath)
	return nil
}

func shellIntegrationSnippet() string {
	return `# OCI Bastion Hopper shell integration.
# Primary workflow:
#   hop vmordws02
#   ssh vmordws02
# Fallback without this helper:
#   oci-hop ssh vmordws02

if command -v hop >/dev/null 2>&1; then
  alias ohop='hop'

  hssh() {
    if [[ $# -lt 1 ]]; then
      print -u2 "Usage: hssh <host> [ssh options...]"
      return 2
    fi

    local host="$1"
    shift
    hop "$host" || return
    ssh "$@" "$host"
  }

  if autoload -Uz compinit 2>/dev/null; then
    if ! (( $+functions[compdef] )); then
      compinit -i
    fi
    eval "$(hop completion zsh 2>/dev/null)"
  fi
fi
`
}

func cmdUpgrade(runInstaller bool, prefix, releaseVersion string) error {
	installCommand := upgradeCommand(prefix, releaseVersion)
	if !runInstaller {
		return emit(map[string]any{
			"ok":      true,
			"dry_run": true,
			"message": "Run with --run to execute the installer command.",
			"command": installCommand,
		})
	}
	cmd := exec.Command("bash", "-lc", strings.Join(installCommand, " "))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	rc := 0
	if err != nil {
		rc = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			rc = ee.ExitCode()
		}
	}
	ok := err == nil
	out := map[string]any{
		"ok":        ok,
		"dry_run":   false,
		"command":   installCommand,
		"exit_code": rc,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
	}
	if !ok {
		out["issue"] = issue{ErrorCode: "upgrade_failed", Message: strings.TrimSpace(stderr.String()), NextCommand: commandName() + " upgrade"}
	}
	if err := emit(out); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "upgrade failed"}
	}
	return nil
}

func emitVersion(format string, verbose bool) error {
	switch format {
	case "json":
		return emit(map[string]any{"ok": true, "version": version, "commit": commit, "date": date})
	case "text", "":
		if verbose {
			fmt.Printf("%s (commit=%s date=%s)\n", version, commit, date)
		} else {
			fmt.Println(version)
		}
		return nil
	default:
		return cliError{code: 2, msg: "version output must be json or text"}
	}
}

func cmdContractCheck(args []string) error {
	if len(args) != 0 {
		return cliError{code: 2, msg: "contract-check accepts no arguments"}
	}
	checks := []commandResult{
		runJSON("oci-context", "auth", "ensure", "--output", "json"),
		runJSON("oci-context", "status", "--cached", "-o", "json"),
		runJSON("bastion-session", "target", "list", "-o", "json"),
	}
	ok := true
	for _, check := range checks {
		ok = ok && check.OK && check.JSON != nil
	}
	if err := emit(map[string]any{"ok": ok, "checks": checks}); err != nil {
		return err
	}
	if !ok {
		return cliError{code: 1, msg: "contract check failed"}
	}
	return nil
}

func runJSON(name string, args ...string) commandResult {
	return runCommand(name, args...)
}

func runCommand(name string, args ...string) commandResult {
	cmdArgs := append([]string{name}, args...)
	cmd := exec.Command(name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	rc := 0
	errorCode := ""
	message := ""
	if err != nil {
		rc = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			rc = ee.ExitCode()
			errorCode = "command_failed"
			message = strings.TrimSpace(stderr.String())
			if message == "" {
				message = err.Error()
			}
		} else {
			var execErr *exec.Error
			if errors.As(err, &execErr) {
				rc = 127
				errorCode = "command_not_found"
				message = execErr.Error()
			} else {
				errorCode = "command_error"
				message = err.Error()
			}
		}
	}
	var raw *json.RawMessage
	trimmed := bytes.TrimSpace(stdout.Bytes())
	if len(trimmed) > 0 && json.Valid(trimmed) {
		cp := json.RawMessage(append([]byte(nil), trimmed...))
		raw = &cp
	}
	return commandResult{Command: cmdArgs, OK: err == nil, ExitCode: rc, Stdout: stdout.String(), Stderr: stderr.String(), ErrorCode: errorCode, Message: message, JSON: raw}
}

func compactReadyLine(host string, ensured, sshConfig commandResult) string {
	fields := []string{"ready", host}
	if privateIP := stringFieldFromJSON(ensured, "target_private_ip"); privateIP != "" {
		fields = append(fields, privateIP)
	} else if hostname := stringFieldFromJSON(sshConfig, "hostname"); hostname != "" {
		fields = append(fields, hostname)
	}
	if proxyJump := firstStringFieldFromJSON(sshConfig, "proxyjump", "proxy_jump"); proxyJump != "" {
		fields = append(fields, "via "+proxyJump)
	} else if proxyJump := firstStringFieldFromJSON(ensured, "proxyjump", "proxy_jump"); proxyJump != "" {
		fields = append(fields, "via "+proxyJump)
	}
	return strings.Join(fields, "  ")
}

func firstStringFieldFromJSON(result commandResult, keys ...string) string {
	for _, key := range keys {
		if value := stringFieldFromJSON(result, key); value != "" {
			return value
		}
	}
	return ""
}

func stringFieldFromJSON(result commandResult, key string) string {
	if result.JSON == nil {
		return ""
	}
	var obj map[string]any
	if json.Unmarshal(*result.JSON, &obj) != nil {
		return ""
	}
	value, _ := obj[key].(string)
	return value
}

func emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func optionalHost(args []string) (string, error) {
	return optionalHostFor("doctor", args)
}

func optionalHostFor(command string, args []string) (string, error) {
	if len(args) > 1 {
		return "", cliError{code: 2, msg: command + " accepts at most one host"}
	}
	if len(args) == 0 {
		return "", nil
	}
	return strings.TrimSpace(args[0]), nil
}

func requiredHostOnly(command string, args []string) (string, error) {
	if len(args) != 1 {
		return "", cliError{code: 2, msg: command + " requires exactly one host"}
	}
	host := strings.TrimSpace(args[0])
	if host == "" {
		return "", cliError{code: 2, msg: "host is required"}
	}
	return host, nil
}

func parseHostIdentity(args []string) (host, identityFile string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--identity-file":
			if i+1 >= len(args) {
				return "", "", cliError{code: 2, msg: "--identity-file requires a value"}
			}
			identityFile = args[i+1]
			i++
		default:
			if host != "" {
				return "", "", cliError{code: 2, msg: "unexpected argument: " + args[i]}
			}
			host = args[i]
		}
	}
	if strings.TrimSpace(host) == "" {
		return "", "", cliError{code: 2, msg: "host is required"}
	}
	return host, identityFile, nil
}

func parseHostIdentityWait(args []string) (host, identityFile, waitTimeout string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--identity-file":
			if i+1 >= len(args) {
				return "", "", "", cliError{code: 2, msg: "--identity-file requires a value"}
			}
			identityFile = args[i+1]
			i++
		case "--wait-timeout":
			if i+1 >= len(args) {
				return "", "", "", cliError{code: 2, msg: "--wait-timeout requires a value"}
			}
			waitTimeout = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", cliError{code: 2, msg: "unknown ensure flag: " + args[i]}
			}
			if host != "" {
				return "", "", "", cliError{code: 2, msg: "unexpected argument: " + args[i]}
			}
			host = args[i]
		}
	}
	if strings.TrimSpace(host) == "" {
		return "", "", "", cliError{code: 2, msg: "host is required"}
	}
	return host, identityFile, waitTimeout, nil
}

func parseRepairArgs(args []string) (host, identityFile string, ensure bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ensure":
			ensure = true
		case "--identity-file":
			if i+1 >= len(args) {
				return "", "", false, cliError{code: 2, msg: "--identity-file requires a value"}
			}
			identityFile = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", false, cliError{code: 2, msg: "unknown repair flag: " + args[i]}
			}
			if host != "" {
				return "", "", false, cliError{code: 2, msg: "unexpected argument: " + args[i]}
			}
			host = args[i]
		}
	}
	if strings.TrimSpace(host) == "" {
		return "", "", false, cliError{code: 2, msg: "host is required"}
	}
	return host, identityFile, ensure, nil
}

func parseTrackArgs(args []string) (host, terraformOutputs string, passthrough []string, err error) {
	if len(args) < 1 {
		return "", "", nil, cliError{code: 2, msg: "track requires <host> <terraform-outputs> or <host> --terraform-dir DIR"}
	}
	host = strings.TrimSpace(args[0])
	if host == "" {
		return "", "", nil, cliError{code: 2, msg: "host is required"}
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--terraform-dir":
			if i+1 >= len(args) {
				return "", "", nil, cliError{code: 2, msg: "--terraform-dir requires a value"}
			}
			if terraformOutputs != "" {
				return "", "", nil, cliError{code: 2, msg: "terraform outputs specified more than once"}
			}
			terraformOutputs = args[i+1]
			i++
		case "--user", "--identity-file":
			if i+1 >= len(args) {
				return "", "", nil, cliError{code: 2, msg: args[i] + " requires a value"}
			}
			passthrough = append(passthrough, args[i], args[i+1])
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", nil, cliError{code: 2, msg: "unknown track flag: " + args[i]}
			}
			if terraformOutputs != "" {
				return "", "", nil, cliError{code: 2, msg: "unexpected argument: " + args[i]}
			}
			terraformOutputs = args[i]
		}
	}
	if strings.TrimSpace(terraformOutputs) == "" {
		return "", "", nil, cliError{code: 2, msg: "terraform outputs path is required"}
	}
	return host, terraformOutputs, passthrough, nil
}

func parseSSHArgs(args []string) (host, identityFile string, dryRun bool, sshArgs []string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			sshArgs = append(sshArgs, args[i+1:]...)
			return requireHost(host, identityFile, dryRun, sshArgs)
		case "--dry-run":
			dryRun = true
		case "--identity-file":
			if i+1 >= len(args) {
				return "", "", false, nil, cliError{code: 2, msg: "--identity-file requires a value"}
			}
			identityFile = args[i+1]
			i++
		default:
			if host != "" {
				sshArgs = append(sshArgs, args[i:]...)
				return requireHost(host, identityFile, dryRun, sshArgs)
			}
			host = args[i]
		}
	}
	return requireHost(host, identityFile, dryRun, sshArgs)
}

func requireHost(host, identityFile string, dryRun bool, sshArgs []string) (string, string, bool, []string, error) {
	if strings.TrimSpace(host) == "" {
		return "", "", false, nil, cliError{code: 2, msg: "host is required"}
	}
	return host, identityFile, dryRun, sshArgs, nil
}

func connectCommandFrom(host string, result commandResult) string {
	if result.JSON != nil {
		var obj map[string]any
		if json.Unmarshal(*result.JSON, &obj) == nil {
			if v, _ := obj["connect_command"].(string); v != "" {
				return v
			}
		}
	}
	return "ssh " + host
}

func versions() map[string]commandResult {
	return map[string]commandResult{
		"oci_hop":         {Command: []string{commandName(), "--version"}, OK: true, ExitCode: 0, Stdout: version + "\n"},
		"oci_context":     runCommand("oci-context", "--version"),
		"bastion_session": runCommand("bastion-session", "--version"),
		"ssh":             runCommand("ssh", "-V"),
	}
}

func firstIssue(message, nextCommand string, results ...commandResult) issue {
	for _, result := range results {
		if len(result.Command) == 0 || result.OK {
			continue
		}
		code := result.ErrorCode
		if code == "" {
			code = "command_failed"
		}
		msg := result.Message
		if msg == "" {
			msg = strings.TrimSpace(result.Stderr)
		}
		if msg == "" {
			msg = message
		}
		return issue{ErrorCode: code, Message: msg, NextCommand: nextCommand}
	}
	return issue{ErrorCode: "not_ok", Message: message, NextCommand: nextCommand}
}

func nextForHost(prefix, host string) string {
	if strings.TrimSpace(host) == "" {
		return strings.TrimSpace(prefix)
	}
	return strings.TrimSpace(prefix) + " " + host
}

func pathsPayload() map[string]any {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	absExe, err := filepath.Abs(exe)
	if err == nil {
		exe = absExe
	}
	paths := map[string]string{
		"executable":         exe,
		"hop_binary":         filepath.Join("/opt/homebrew", "bin", primaryCommand),
		"qualified_binary":   filepath.Join("/opt/homebrew", "bin", qualifiedCommand),
		"home":               home,
		"oci_context_config": filepath.Join(home, ".oci-context", "config.yml"),
		"oci_config":         filepath.Join(home, ".oci", "config"),
		"ssh_config":         filepath.Join(home, ".ssh", "config"),
		"ssh_dir":            filepath.Join(home, ".ssh"),
		"bastion_cache":      filepath.Join(home, ".cache", "bastion-session"),
		"install_script":     "https://raw.githubusercontent.com/adrianmross/oci-hop/main/install.sh",
	}
	return map[string]any{"ok": true, "paths": paths}
}

func upgradeCommand(prefix, releaseVersion string) []string {
	env := []string{}
	if prefix != "" {
		env = append(env, "PREFIX="+shellQuote(prefix))
	}
	if releaseVersion != "" {
		env = append(env, "VERSION="+shellQuote(releaseVersion))
	}
	cmd := []string{"curl", "-fsSL", "https://raw.githubusercontent.com/adrianmross/oci-hop/main/install.sh", "|"}
	if len(env) == 0 {
		return append(cmd, "bash")
	}
	cmd = append(cmd, env...)
	return append(cmd, "bash")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func hostCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	targets := runJSON("bastion-session", "target", "list", "-o", "json")
	if !targets.OK || targets.JSON == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTargets(*targets.JSON, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func pathAfterHostCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return hostCompletion(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveDefault
}

func fileCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveDefault
}

func dirCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func outputFormatCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"json", "text"}, cobra.ShellCompDirectiveNoFileComp
}

func completeTargets(raw json.RawMessage, prefix string) []string {
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, item := range items {
		for _, key := range []string{"name", "host", "hostname"} {
			name, _ := item[key].(string)
			if name != "" && strings.HasPrefix(name, prefix) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

type cliError struct {
	code int
	msg  string
}

func (e cliError) Error() string { return e.msg }

func exitCode(err error) int {
	var ce cliError
	if errors.As(err, &ce) && ce.code > 0 {
		return ce.code
	}
	return 1
}

func syscallExec(name string, argv []string) error {
	cmd := exec.Command(name, argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
