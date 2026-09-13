package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"systemdoc/internal/dashboard"
	"systemdoc/internal/remote"
)

var version = "dev"

func main() {
	user := flag.Bool("user", false, "Inspect user services (systemd on Linux, launchd GUI agents on macOS)")
	docker := flag.Bool("docker", false, "Open Docker mode")
	activeOnly := flag.Bool("active-only", false, "Show only active services or running containers")
	filter := flag.String("filter", "", "Initial workload filter (for example state:failed)")
	showVersion := flag.Bool("version", false, "Print version")
	dockerContext := flag.String("docker-context", "", "Pin Docker context for this session")
	host := flag.String("ssh", "", "Run the suite on an SSH host or user@host")
	setupKey := flag.Bool("setup-ssh-key", false, "Install a public SSH key with ssh-copy-id before connecting")
	sshUser := flag.String("ssh-user", "", "SSH login username (default: SSH configuration)")
	port := flag.Int("ssh-port", 0, "SSH port (default: SSH configuration)")
	identity := flag.String("identity", "", "SSH identity file")
	remoteBin := flag.String("remote-bin", "systemdoc", "Remote executable name or absolute path")
	upload := flag.Bool("upload", false, "Upload a temporary matching static binary for this SSH session")
	uploadBinary := flag.String("upload-binary", "", "Use this matching platform binary with --upload")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Unexpected arguments; use --help for options.")
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("systemdoc " + version)
		return
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH != "arm64" {
		fmt.Fprintln(os.Stderr, "macOS support requires Apple Silicon (arm64).")
		os.Exit(2)
	}
	if *host != "" {
		args := []string{}
		if *activeOnly {
			args = append(args, "--active-only")
		}
		if *user {
			args = append(args, "--user")
		}
		if *docker {
			args = append(args, "--docker")
		}
		if *filter != "" {
			args = append(args, "--filter", *filter)
		}
		if *dockerContext != "" {
			args = append(args, "--docker-context", *dockerContext)
		}
		options := remote.Options{Host: *host, User: *sshUser, Port: *port, Identity: *identity, Binary: *remoteBin, Upload: *upload, UploadBinary: *uploadBinary, Args: args}
		if *setupKey {
			if options.Identity == "" {
				path, err := remote.DefaultKey()
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				options.Identity = path
			}
			if err := remote.InstallKey(options, options.Identity); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		err := remote.Run(options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *setupKey {
		fmt.Fprintln(os.Stderr, "--setup-ssh-key requires --ssh")
		os.Exit(2)
	}
	if *sshUser != "" {
		fmt.Fprintln(os.Stderr, "--ssh-user requires --ssh")
		os.Exit(2)
	}
	if *upload || *uploadBinary != "" {
		fmt.Fprintln(os.Stderr, "--upload requires --ssh; --upload-binary also requires --upload")
		os.Exit(2)
	}
	if *dockerContext != "" {
		os.Setenv("DOCKER_CONTEXT", *dockerContext)
	}
	if err := dashboard.Run(dashboard.Options{User: *user, Docker: *docker, Filter: *filter, ActiveOnly: *activeOnly}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
