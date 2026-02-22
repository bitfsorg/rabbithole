// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
)

// shellCommands is the list of all shell command names for tab completion.
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "help", "quit", "exit",
}

// runShell handles the "bitfs shell" command.
// Provides an FTP-style interactive REPL with line editing, history,
// and tab completion (commands + remote/local paths).
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

	cwd := "/"
	localCwd, _ := os.Getwd()

	completer := &shellCompleter{
		commands: shellCommands,
		state:    eng.State,
		cwd:      cwd,
		localCwd: localCwd,
	}

	historyFile := filepath.Join(*dataDir, "shell_history")
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          fmt.Sprintf("bitfs:%s> ", cwd),
		HistoryFile:     historyFile,
		HistoryLimit:    500,
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing shell: %v\n", err)
		return exitError
	}
	defer rl.Close()

	fmt.Fprintf(rl.Stdout(), "BitFS Shell (vault %d). Type 'help' for commands, 'quit' to exit.\n", vaultIdx)

	for {
		line, err := rl.ReadLine()
		if err == readline.ErrInterrupt {
			continue // Ctrl-C: cancel current line.
		}
		if err == io.EOF {
			fmt.Fprintln(rl.Stdout(), "Bye.")
			return exitSuccess
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitError
		}

		line = strings.TrimSpace(line)
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
			completer.cwd = cwd
			rl.SetPrompt(fmt.Sprintf("bitfs:%s> ", cwd))
		case "lcd":
			if len(cmdArgs) == 0 {
				fmt.Println(localCwd)
			} else {
				target := cmdArgs[0]
				if !filepath.IsAbs(target) {
					target = filepath.Join(localCwd, target)
				}
				target = filepath.Clean(target)
				info, statErr := os.Stat(target)
				if statErr != nil || !info.IsDir() {
					fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", target)
					continue
				}
				localCwd = target
				completer.localCwd = localCwd
				fmt.Printf("Local directory: %s\n", localCwd)
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
			result, mkErr := eng.Mkdir(&engine.MkdirOpts{VaultIndex: vaultIdx, Path: path})
			if mkErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", mkErr)
			} else {
				fmt.Println(result.Message)
			}
		case "put":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: put <local-file> <remote-path>")
				continue
			}
			localFile := cmdArgs[0]
			if !filepath.IsAbs(localFile) {
				localFile = filepath.Join(localCwd, localFile)
			}
			remotePath := resolvePath(cwd, cmdArgs[1])
			result, putErr := eng.PutFile(&engine.PutOpts{
				VaultIndex: vaultIdx,
				LocalFile:  localFile,
				RemotePath: remotePath,
				Access:     "free",
			})
			if putErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", putErr)
			} else {
				fmt.Println(result.Message)
			}
		case "rm":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: rm <path>")
				continue
			}
			path := resolvePath(cwd, cmdArgs[0])
			result, rmErr := eng.Remove(&engine.RemoveOpts{VaultIndex: vaultIdx, Path: path})
			if rmErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", rmErr)
			} else {
				fmt.Println(result.Message)
			}
		case "mv":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: mv <src> <dst>")
				continue
			}
			result, mvErr := eng.Move(&engine.MoveOpts{
				VaultIndex: vaultIdx,
				SrcPath:    resolvePath(cwd, cmdArgs[0]),
				DstPath:    resolvePath(cwd, cmdArgs[1]),
			})
			if mvErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", mvErr)
			} else {
				fmt.Println(result.Message)
			}
		case "cp":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: cp <src> <dst>")
				continue
			}
			result, cpErr := eng.Copy(&engine.CopyOpts{
				VaultIndex: vaultIdx,
				SrcPath:    resolvePath(cwd, cmdArgs[0]),
				DstPath:    resolvePath(cwd, cmdArgs[1]),
			})
			if cpErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", cpErr)
			} else {
				fmt.Println(result.Message)
			}
		case "link":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: link <target> <link-path> [--soft]")
				continue
			}
			soft := len(cmdArgs) > 2 && cmdArgs[2] == "--soft"
			result, lnErr := eng.Link(&engine.LinkOpts{
				VaultIndex: vaultIdx,
				TargetPath: resolvePath(cwd, cmdArgs[0]),
				LinkPath:   resolvePath(cwd, cmdArgs[1]),
				Soft:       soft,
			})
			if lnErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", lnErr)
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
			result, sellErr := eng.Sell(&engine.SellOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
				PricePerKB: price,
			})
			if sellErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", sellErr)
			} else {
				fmt.Println(result.Message)
			}
		case "encrypt":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: encrypt <path>")
				continue
			}
			result, encErr := eng.EncryptNode(&engine.EncryptOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
			})
			if encErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", encErr)
			} else {
				fmt.Println(result.Message)
			}
		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
		}
	}
}

func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                List directory contents
  cd [path]                Change remote directory
  lcd [path]               Change local directory (or print current)
  pwd                      Print remote working directory
  mkdir <path>             Create directory
  put <local> <remote>     Upload file
  rm <path>                Remove file/directory
  mv <src> <dst>           Move/rename
  cp <src> <dst>           Copy file
  link <target> <path>     Create hard link (--soft for symlink)
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
