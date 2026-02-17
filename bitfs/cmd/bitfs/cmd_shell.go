// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
)

// runShell handles the "bitfs shell" command.
// Provides an FTP-style interactive REPL.
func runShell(args []string) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	eng, err := engine.New(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer eng.Close()

	vaultIdx, err := eng.ResolveVaultIndex(*vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	fmt.Printf("BitFS Shell (vault %d). Type 'help' for commands, 'quit' to exit.\n", vaultIdx)

	cwd := "/"
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Printf("bitfs:%s> ", cwd)
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]
		cmdArgs := parts[1:]

		switch cmd {
		case "help":
			shellHelp()
		case "quit", "exit":
			fmt.Println("Bye.")
			return exitSuccess
		case "pwd":
			fmt.Println(cwd)
		case "cd":
			if len(cmdArgs) == 0 {
				cwd = "/"
			} else {
				target := cmdArgs[0]
				if !strings.HasPrefix(target, "/") {
					target = cwd + "/" + target
				}
				target = cleanPath(target)
				cwd = target
			}
		case "ls":
			dir := cwd
			if len(cmdArgs) > 0 {
				dir = resolvePath(cwd, cmdArgs[0])
			}
			shellLs(eng, dir)
		case "mkdir":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: mkdir <path>")
				continue
			}
			path := resolvePath(cwd, cmdArgs[0])
			result, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: vaultIdx, Path: path})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "put":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: put <local-file> <remote-path>")
				continue
			}
			remotePath := resolvePath(cwd, cmdArgs[1])
			result, err := eng.PutFile(&engine.PutOpts{
				VaultIndex: vaultIdx,
				LocalFile:  cmdArgs[0],
				RemotePath: remotePath,
				Access:     "free",
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "rm":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: rm <path>")
				continue
			}
			path := resolvePath(cwd, cmdArgs[0])
			result, err := eng.Remove(&engine.RemoveOpts{VaultIndex: vaultIdx, Path: path})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "mv":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: mv <src> <dst>")
				continue
			}
			result, err := eng.Move(&engine.MoveOpts{
				VaultIndex: vaultIdx,
				SrcPath:    resolvePath(cwd, cmdArgs[0]),
				DstPath:    resolvePath(cwd, cmdArgs[1]),
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "link":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: link <target> <link-path> [--soft]")
				continue
			}
			soft := len(cmdArgs) > 2 && cmdArgs[2] == "--soft"
			result, err := eng.Link(&engine.LinkOpts{
				VaultIndex: vaultIdx,
				TargetPath: resolvePath(cwd, cmdArgs[0]),
				LinkPath:   resolvePath(cwd, cmdArgs[1]),
				Soft:       soft,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "sell":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: sell <path> <price-sats-per-kb>")
				continue
			}
			var price uint64
			_, _ = fmt.Sscanf(cmdArgs[1], "%d", &price)
			if price == 0 {
				fmt.Println("Error: price must be positive")
				continue
			}
			result, err := eng.Sell(&engine.SellOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
				PricePerKB: price,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		case "encrypt":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: encrypt <path>")
				continue
			}
			result, err := eng.EncryptNode(&engine.EncryptOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				fmt.Println(result.Message)
			}
		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
		}
	}

	return exitSuccess
}

func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                List directory contents
  cd [path]                Change directory
  pwd                      Print working directory
  mkdir <path>             Create directory
  put <local> <remote>     Upload file
  rm <path>                Remove file/directory
  mv <src> <dst>           Move/rename
  link <target> <path>     Create hard link
  sell <path> <price>      Set price (sats/KB)
  encrypt <path>           Encrypt (FREE -> PRIVATE)
  help                     Show this help
  quit                     Exit shell`)
}

func shellLs(eng *engine.Engine, dir string) {
	node := eng.State.FindNodeByPath(dir)
	if node == nil {
		fmt.Printf("Not found: %s\n", dir)
		return
	}
	if node.Type != "dir" {
		fmt.Printf("%s  %s\n", node.Type, dir)
		return
	}
	if len(node.Children) == 0 {
		fmt.Println("(empty)")
		return
	}
	for _, c := range node.Children {
		fmt.Printf("  %s  %s\n", c.Type, c.Name)
	}
}

// resolvePath resolves a path relative to cwd.
func resolvePath(cwd, p string) string {
	if strings.HasPrefix(p, "/") {
		return cleanPath(p)
	}
	return cleanPath(cwd + "/" + p)
}

// cleanPath normalizes a path, removing double slashes and trailing slashes.
func cleanPath(p string) string {
	parts := strings.Split(p, "/")
	var clean []string
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(clean) > 0 {
				clean = clean[:len(clean)-1]
			}
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}
