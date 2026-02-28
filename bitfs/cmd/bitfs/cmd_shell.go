// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"

	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/bitfs/internal/publish"
	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// validAccessModes contains the accepted access mode strings.
var validAccessModes = map[string]bool{
	"free":    true,
	"private": true,
	"paid":    true,
}

// validateAccessMode checks if a mode string is a valid access mode.
func validateAccessMode(mode string) error {
	if !validAccessModes[mode] {
		return fmt.Errorf("invalid access mode %q: must be free, private, or paid", mode)
	}
	return nil
}

// ensureHistoryFilePermissions restricts the history file to owner-only access.
func ensureHistoryFilePermissions(path string) {
	_ = os.Chmod(path, 0600)
}

// shellCommands is the list of all shell command names for tab completion.
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "cat", "get", "mget", "mput", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "decrypt", "sales", "publish", "unpublish", "help", "quit", "exit",
}

// shellAction signals the REPL loop how to proceed after a command.
type shellAction int

const (
	shellContinue shellAction = iota // continue the REPL loop
	shellExit                        // break out of the REPL loop (quit/exit)
)

// shellCtx holds mutable state shared between the REPL loop and command dispatch.
type shellCtx struct {
	eng      *vault.Vault
	vaultIdx uint32
	cwd      string
	localCwd string
}

// shellExecCmd dispatches a single shell command and returns a shellAction.
// Extracted from runShell to enable direct unit testing of every command.
func shellExecCmd(ctx *shellCtx, cmd string, args []string) shellAction {
	switch cmd {
	case "help":
		shellHelp()
	case "quit", "exit":
		fmt.Println("Bye.")
		return shellExit
	case "pwd":
		fmt.Println(ctx.cwd)
	case "cd":
		if len(args) == 0 {
			ctx.cwd = "/"
		} else {
			target := args[0]
			if !strings.HasPrefix(target, "/") {
				target = ctx.cwd + "/" + target
			}
			target = cleanPath(target)
			if target != "/" {
				node := ctx.eng.State.FindNodeByPath(target)
				if node == nil {
					fmt.Fprintf(os.Stderr, "cd: %s: no such directory\n", target)
					return shellContinue
				}
				if node.Type != "dir" {
					fmt.Fprintf(os.Stderr, "cd: %s: not a directory\n", target)
					return shellContinue
				}
			}
			ctx.cwd = target
		}
	case "lcd":
		if len(args) == 0 {
			fmt.Println(ctx.localCwd)
		} else {
			target := args[0]
			if !filepath.IsAbs(target) {
				target = filepath.Join(ctx.localCwd, target)
			}
			target = filepath.Clean(target)
			info, statErr := os.Stat(target)
			if statErr != nil || !info.IsDir() {
				fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", target)
				return shellContinue
			}
			ctx.localCwd = target
			fmt.Printf("Local directory: %s\n", ctx.localCwd)
		}
	case "ls":
		dir := ctx.cwd
		if len(args) > 0 {
			dir = resolvePath(ctx.cwd, args[0])
		}
		shellLs(ctx.eng, dir)
	case "mkdir":
		if len(args) < 1 {
			fmt.Println("Usage: mkdir <path>")
			return shellContinue
		}
		path := resolvePath(ctx.cwd, args[0])
		result, mkErr := ctx.eng.Mkdir(&vault.MkdirOpts{VaultIndex: ctx.vaultIdx, Path: path})
		if mkErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", mkErr)
		} else {
			fmt.Println(result.Message)
		}
	case "put":
		if len(args) < 2 {
			fmt.Println("Usage: put <local-file> <remote-path> [free|private]")
			return shellContinue
		}
		localFile := args[0]
		if !filepath.IsAbs(localFile) {
			localFile = filepath.Join(ctx.localCwd, localFile)
		}
		remotePath := resolvePath(ctx.cwd, args[1])
		access := "free"
		if len(args) > 2 && args[2] == "private" {
			access = "private"
		}
		result, putErr := ctx.eng.PutFile(&vault.PutOpts{
			VaultIndex: ctx.vaultIdx,
			LocalFile:  localFile,
			RemotePath: remotePath,
			Access:     access,
		})
		if putErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", putErr)
		} else {
			fmt.Println(result.Message)
		}
	case "rm":
		if len(args) < 1 {
			fmt.Println("Usage: rm [-r] <path>")
			return shellContinue
		}
		recursive := false
		pathArgs := args
		for i, a := range args {
			if a == "-r" || a == "--recursive" {
				recursive = true
				pathArgs = make([]string, 0, len(args)-1)
				pathArgs = append(pathArgs, args[:i]...)
				pathArgs = append(pathArgs, args[i+1:]...)
				break
			}
		}
		if len(pathArgs) < 1 {
			fmt.Println("Usage: rm [-r] <path>")
			return shellContinue
		}
		rmPath := resolvePath(ctx.cwd, pathArgs[0])
		if recursive {
			if err := shellRemoveRecursive(ctx.eng, ctx.vaultIdx, rmPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		} else {
			result, rmErr := ctx.eng.Remove(&vault.RemoveOpts{VaultIndex: ctx.vaultIdx, Path: rmPath})
			if rmErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", rmErr)
			} else {
				fmt.Println(result.Message)
			}
		}
	case "mv":
		if len(args) < 2 {
			fmt.Println("Usage: mv <src> <dst>")
			return shellContinue
		}
		srcPath := resolvePath(ctx.cwd, args[0])
		dstPath := resolvePath(ctx.cwd, args[1])

		// Warn about capsule invalidation on cross-directory mv of paid files.
		if path.Dir(srcPath) != path.Dir(dstPath) {
			srcNode := ctx.eng.State.FindNodeByPath(srcPath)
			if srcNode != nil && srcNode.Access == "paid" {
				fmt.Println("WARNING: Moving this paid file will invalidate existing capsules.")
				fmt.Println("Buyers will need to re-purchase access at the new location.")
				fmt.Print("Continue? [y/N] ")
				var confirm string
				_, _ = fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					fmt.Println("Move canceled.")
					return shellContinue
				}
			}
		}

		result, mvErr := ctx.eng.Move(&vault.MoveOpts{
			VaultIndex: ctx.vaultIdx,
			SrcPath:    srcPath,
			DstPath:    dstPath,
			Force:      true,
		})
		if mvErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", mvErr)
		} else {
			fmt.Println(result.Message)
		}
	case "cp":
		if len(args) < 2 {
			fmt.Println("Usage: cp <src> <dst>")
			return shellContinue
		}
		result, cpErr := ctx.eng.Copy(&vault.CopyOpts{
			VaultIndex: ctx.vaultIdx,
			SrcPath:    resolvePath(ctx.cwd, args[0]),
			DstPath:    resolvePath(ctx.cwd, args[1]),
		})
		if cpErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", cpErr)
		} else {
			fmt.Println(result.Message)
		}
	case "link":
		if len(args) < 2 {
			fmt.Println("Usage: link <target> <link-path> [-s|--soft]")
			return shellContinue
		}
		soft := false
		posArgs := make([]string, 0, len(args))
		for _, a := range args {
			if a == "-s" || a == "--soft" {
				soft = true
			} else {
				posArgs = append(posArgs, a)
			}
		}
		if len(posArgs) < 2 {
			fmt.Println("Usage: link <target> <link-path> [-s|--soft]")
			return shellContinue
		}
		result, lnErr := ctx.eng.Link(&vault.LinkOpts{
			VaultIndex: ctx.vaultIdx,
			TargetPath: resolvePath(ctx.cwd, posArgs[0]),
			LinkPath:   resolvePath(ctx.cwd, posArgs[1]),
			Soft:       soft,
		})
		if lnErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", lnErr)
		} else {
			fmt.Println(result.Message)
		}
	case "sell":
		if len(args) < 2 {
			fmt.Println("Usage: sell <path> <price-sats-per-kb> [--recursive]")
			return shellContinue
		}
		recursive := false
		cleanArgs := make([]string, 0, len(args))
		for _, a := range args {
			if a == "-r" || a == "--recursive" {
				recursive = true
			} else {
				cleanArgs = append(cleanArgs, a)
			}
		}
		if len(cleanArgs) < 2 {
			fmt.Println("Usage: sell <path> <price-sats-per-kb> [--recursive]")
			return shellContinue
		}
		var price uint64
		if _, err := fmt.Sscanf(cleanArgs[1], "%d", &price); err != nil || price == 0 {
			fmt.Println("Error: price must be a positive integer")
			return shellContinue
		}
		sellPath := resolvePath(ctx.cwd, cleanArgs[0])
		if recursive {
			if err := shellSellRecursive(ctx.eng, ctx.vaultIdx, sellPath, price); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		} else {
			result, sellErr := ctx.eng.Sell(&vault.SellOpts{
				VaultIndex: ctx.vaultIdx,
				Path:       sellPath,
				PricePerKB: price,
			})
			if sellErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", sellErr)
			} else {
				fmt.Println(result.Message)
			}
		}
	case "cat":
		if len(args) < 1 {
			fmt.Println("Usage: cat <path>")
			return shellContinue
		}
		remotePath := resolvePath(ctx.cwd, args[0])
		force := len(args) > 1 && args[1] == "--force"
		reader, info, catErr := ctx.eng.Cat(&vault.CatOpts{
			Path: remotePath,
		})
		switch {
		case catErr != nil:
			fmt.Fprintf(os.Stderr, "Error: %v\n", catErr)
		case !force && !isTextMime(info.MimeType):
			fmt.Fprintf(os.Stderr, "Binary file (%s, %d bytes). Use 'cat <path> --force' or 'get' to download.\n", info.MimeType, info.FileSize)
		default:
			if _, cpErr := io.Copy(os.Stdout, reader); cpErr != nil {
				fmt.Fprintf(os.Stderr, "Error writing output: %v\n", cpErr)
			}
		}
	case "get":
		if len(args) < 1 {
			fmt.Println("Usage: get <remote> [local]")
			return shellContinue
		}
		remotePath := resolvePath(ctx.cwd, args[0])
		localPath := ""
		if len(args) > 1 {
			localPath = args[1]
			if !filepath.IsAbs(localPath) {
				localPath = filepath.Join(ctx.localCwd, localPath)
			}
		}
		result, getErr := ctx.eng.Get(&vault.GetOpts{
			VaultIndex: ctx.vaultIdx,
			RemotePath: remotePath,
			LocalDir:   ctx.localCwd,
			LocalPath:  localPath,
		})
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", getErr)
		} else {
			fmt.Println(result.Message)
		}
	case "mget":
		if len(args) < 1 {
			fmt.Println("Usage: mget <remote-dir> [local-dir]")
			return shellContinue
		}
		remotePath := resolvePath(ctx.cwd, args[0])
		localDir := ctx.localCwd
		if len(args) > 1 {
			localDir = args[1]
			if !filepath.IsAbs(localDir) {
				localDir = filepath.Join(ctx.localCwd, localDir)
			}
		}
		mgResult, mgetErr := doMget(ctx.eng, ctx.vaultIdx, remotePath, localDir)
		if mgetErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", mgetErr)
		} else {
			fmt.Printf("Downloaded %d files, created %d directories\n",
				mgResult.FilesDownloaded, mgResult.DirsCreated)
			for _, e := range mgResult.Errors {
				fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
			}
		}
	case "mput":
		if len(args) < 1 {
			fmt.Println("Usage: mput <local-dir> [remote-dir]")
			return shellContinue
		}
		localDir := args[0]
		if !filepath.IsAbs(localDir) {
			localDir = filepath.Join(ctx.localCwd, localDir)
		}
		remoteDir := ctx.cwd
		if len(args) > 1 {
			remoteDir = resolvePath(ctx.cwd, args[1])
		}
		access := "free"
		if len(args) > 2 {
			access = args[2]
		}
		if err := validateAccessMode(access); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return shellContinue
		}
		mpResult, mputErr := doMput(ctx.eng, ctx.vaultIdx, localDir, remoteDir, access)
		if mputErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", mputErr)
		} else {
			fmt.Printf("Uploaded %d files, created %d directories\n",
				mpResult.FilesUploaded, mpResult.DirsCreated)
			for _, e := range mpResult.Errors {
				fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
			}
		}
	case "publish":
		if len(args) == 0 {
			// List all publish bindings.
			bindings := ctx.eng.State.ListPublishBindings()
			if len(bindings) == 0 {
				fmt.Println("No published domains.")
			} else {
				for _, b := range bindings {
					verified := ""
					if b.Verified {
						verified = " [verified]"
					}
					fmt.Printf("  %s -> vault %d%s\n", b.Domain, b.VaultIndex, verified)
				}
			}
		} else {
			domain := args[0]
			dns := publish.DefaultDNSResolver()
			result, pubErr := publish.Publish(ctx.eng, dns, &publish.PublishOpts{
				VaultIndex: ctx.vaultIdx,
				Domain:     domain,
			})
			if pubErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", pubErr)
			} else {
				fmt.Println(result.Message)
			}
		}
	case "unpublish":
		if len(args) < 1 {
			fmt.Println("Usage: unpublish <domain>")
			return shellContinue
		}
		domain := args[0]
		result, unpubErr := publish.Unpublish(ctx.eng, &publish.UnpublishOpts{
			Domain: domain,
		})
		if unpubErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", unpubErr)
		} else {
			fmt.Println(result.Message)
		}
	case "encrypt":
		if len(args) < 1 {
			fmt.Println("Usage: encrypt <path>")
			return shellContinue
		}
		result, encErr := ctx.eng.EncryptNode(&vault.EncryptOpts{
			VaultIndex: ctx.vaultIdx,
			Path:       resolvePath(ctx.cwd, args[0]),
		})
		if encErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", encErr)
		} else {
			fmt.Println(result.Message)
		}
	case "decrypt":
		if len(args) < 1 {
			fmt.Println("Usage: decrypt <path>")
			return shellContinue
		}
		result, decErr := ctx.eng.DecryptNode(&vault.DecryptOpts{
			Path: resolvePath(ctx.cwd, args[0]),
		})
		if decErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", decErr)
		} else {
			fmt.Println(result.Message)
		}
	case "sales":
		daemonURL := "http://localhost:8080" // default daemon port
		cl := client.New(daemonURL)
		records, salesErr := cl.GetSales("all", 50)
		if salesErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v (is daemon running?)\n", salesErr)
			return shellContinue
		}
		if len(records) == 0 {
			fmt.Println("No sales records.")
			return shellContinue
		}
		fmt.Printf("%-36s  %10s  %5s  %s\n", "INVOICE", "PRICE(sat)", "PAID", "KEY_HASH")
		for _, r := range records {
			paid := "no"
			if r.Paid {
				paid = "yes"
			}
			kh := r.KeyHash
			if len(kh) > 16 {
				kh = kh[:16] + "..."
			}
			fmt.Printf("%-36s  %10d  %5s  %s\n", r.InvoiceID, r.Price, paid, kh)
		}
	default:
		fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
	}
	return shellContinue
}

// runShell handles the "bitfs shell" command.
// Provides an FTP-style interactive REPL with line editing, history,
// and tab completion (commands + remote/local paths).
func runShell(args []string) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	vaultName := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	eng, err := vault.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = eng.Close() }()

	vaultIdx, err := eng.ResolveVaultIndex(*vaultName)
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
	defer func() { _ = rl.Close() }()
	ensureHistoryFilePermissions(historyFile)

	_, _ = fmt.Fprintf(rl.Stdout(), "BitFS Shell (vault %d). Type 'help' for commands, 'quit' to exit.\n", vaultIdx)

	ctx := &shellCtx{eng: eng, vaultIdx: vaultIdx, cwd: cwd, localCwd: localCwd}

	for {
		line, err := rl.ReadLine()
		if errors.Is(err, readline.ErrInterrupt) {
			continue // Ctrl-C: cancel current line.
		}
		if errors.Is(err, io.EOF) {
			_, _ = fmt.Fprintln(rl.Stdout(), "Bye.")
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

		action := shellExecCmd(ctx, cmd, cmdArgs)
		if action == shellExit {
			return exitSuccess
		}

		// Sync mutable state back to completer/prompt after cd/lcd.
		completer.cwd = ctx.cwd
		completer.localCwd = ctx.localCwd
		rl.SetPrompt(fmt.Sprintf("bitfs:%s> ", ctx.cwd))
	}
}

func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                     List directory contents
  cd [path]                     Change remote directory
  lcd [path]                    Change local directory (or print current)
  pwd                           Print remote working directory
  cat <path>                    View file contents (--force for binary)
  get <remote> [local]          Download file to local disk
  mget <dir> [local-dir]        Download directory recursively
  mput <dir> [remote-dir]       Upload directory recursively
  mkdir <path>                  Create directory
  put <local> <remote> [access] Upload file (access: free|private, default free)
  rm [-r] <path>                Remove file/directory (-r for recursive)
  mv <src> <dst>                Move/rename
  cp <src> <dst>                Copy file
  link <target> <path> [-s]     Create link (-s for soft/symlink)
  sell <path> <price> [-r]      Set price sats/KB (-r for recursive)
  encrypt <path>                Encrypt (FREE -> PRIVATE)
  decrypt <path>                Decrypt (PRIVATE -> FREE)
  sales                         View sales records (requires daemon)
  publish [domain]              List or bind domain via DNSLink
  unpublish <domain>            Remove domain binding
  help                          Show this help
  quit                          Exit shell`)
}

func shellLs(eng *vault.Vault, dir string) {
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

// shellRemoveRecursive removes a path and all its children bottom-up.
func shellRemoveRecursive(eng *vault.Vault, vaultIdx uint32, path string) error {
	ns := eng.State.FindNodeByPath(path)
	if ns == nil {
		return fmt.Errorf("vault: node %q not found", path)
	}
	// Remove children first (depth-first).
	if ns.Type == "dir" {
		for _, child := range ns.Children {
			childPath := path + "/" + child.Name
			if err := shellRemoveRecursive(eng, vaultIdx, childPath); err != nil {
				return err
			}
		}
	}
	result, err := eng.Remove(&vault.RemoveOpts{VaultIndex: vaultIdx, Path: path})
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	return nil
}

// shellSellRecursive applies a price to a path and all file descendants.
func shellSellRecursive(eng *vault.Vault, vaultIdx uint32, path string, price uint64) error {
	ns := eng.State.FindNodeByPath(path)
	if ns == nil {
		return fmt.Errorf("vault: node %q not found", path)
	}
	if ns.Type == "dir" {
		for _, child := range ns.Children {
			childPath := path + "/" + child.Name
			if err := shellSellRecursive(eng, vaultIdx, childPath, price); err != nil {
				return err
			}
		}
		return nil // don't sell directories themselves
	}
	result, err := eng.Sell(&vault.SellOpts{
		VaultIndex: vaultIdx,
		Path:       path,
		PricePerKB: price,
	})
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	return nil
}
