// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
)

// newTestShellCtx creates a shellCtx backed by a pre-populated vault for testing.
func newTestShellCtx(t *testing.T) *shellCtx {
	t.Helper()
	v, _ := populateTestVault(t)
	t.Cleanup(func() { v.Close() })
	return &shellCtx{
		eng:      v,
		vaultIdx: 0,
		cwd:      "/",
		localCwd: t.TempDir(),
	}
}

// ---------------------------------------------------------------------------
// Basic commands: help, quit, exit, pwd, unknown
// ---------------------------------------------------------------------------

func TestShellExec_Help(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "help", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Quit(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "quit", nil)
	assert.Equal(t, shellExit, action)
}

func TestShellExec_Exit(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "exit", nil)
	assert.Equal(t, shellExit, action)
}

func TestShellExec_Pwd(t *testing.T) {
	ctx := newTestShellCtx(t)
	ctx.cwd = "/some/path"
	action := shellExecCmd(ctx, "pwd", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_UnknownCommand(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "bogus", nil)
	assert.Equal(t, shellContinue, action)
}

// ---------------------------------------------------------------------------
// cd — directory navigation
// ---------------------------------------------------------------------------

func TestShellExec_Cd_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	ctx.cwd = "/subdir"
	action := shellExecCmd(ctx, "cd", nil)
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/", ctx.cwd)
}

func TestShellExec_Cd_ToSubdir(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cd", []string{"subdir"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/subdir", ctx.cwd)
}

func TestShellExec_Cd_AbsolutePath(t *testing.T) {
	ctx := newTestShellCtx(t)
	ctx.cwd = "/subdir"
	action := shellExecCmd(ctx, "cd", []string{"/"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/", ctx.cwd)
}

func TestShellExec_Cd_NotFound(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cd", []string{"nonexistent"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/", ctx.cwd) // unchanged
}

func TestShellExec_Cd_NotDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cd", []string{"hello.txt"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/", ctx.cwd) // unchanged
}

// ---------------------------------------------------------------------------
// lcd — local directory change
// ---------------------------------------------------------------------------

func TestShellExec_Lcd_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "lcd", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Lcd_ValidDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	sub := filepath.Join(ctx.localCwd, "sub")
	require.NoError(t, os.MkdirAll(sub, 0755))
	action := shellExecCmd(ctx, "lcd", []string{sub})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, sub, ctx.localCwd)
}

func TestShellExec_Lcd_RelativePath(t *testing.T) {
	ctx := newTestShellCtx(t)
	sub := filepath.Join(ctx.localCwd, "rel")
	require.NoError(t, os.MkdirAll(sub, 0755))
	action := shellExecCmd(ctx, "lcd", []string{"rel"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, sub, ctx.localCwd)
}

func TestShellExec_Lcd_NotDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	f := filepath.Join(ctx.localCwd, "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0644))
	orig := ctx.localCwd
	action := shellExecCmd(ctx, "lcd", []string{f})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, orig, ctx.localCwd) // unchanged
}

func TestShellExec_Lcd_Nonexistent(t *testing.T) {
	ctx := newTestShellCtx(t)
	orig := ctx.localCwd
	action := shellExecCmd(ctx, "lcd", []string{"/does/not/exist"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, orig, ctx.localCwd) // unchanged
}

// ---------------------------------------------------------------------------
// ls — list directory
// ---------------------------------------------------------------------------

func TestShellExec_Ls_Cwd(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "ls", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Ls_WithArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "ls", []string{"subdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Ls_Nonexistent(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "ls", []string{"nope"})
	assert.Equal(t, shellContinue, action)
}

// ---------------------------------------------------------------------------
// Usage-only branches (no args → print usage)
// ---------------------------------------------------------------------------

func TestShellExec_Mkdir_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mkdir", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "put", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_OneArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "put", []string{"file.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "rm", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_OnlyFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	// "-r" with no path should print usage
	action := shellExecCmd(ctx, "rm", []string{"-r"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mv_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mv", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mv_OneArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mv", []string{"src"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cp_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cp", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cp_OneArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cp", []string{"src"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "link", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_OneArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "link", []string{"target"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_OnlySoftFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	// "-s" + one path = not enough positional args
	action := shellExecCmd(ctx, "link", []string{"-s", "target"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "sell", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_OneArg(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"/file"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_InvalidPrice(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"/file", "abc"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_ZeroPrice(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"/file", "0"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cat_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "cat", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Get_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "get", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mget_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mget", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mput", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Encrypt_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "encrypt", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Decrypt_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "decrypt", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Unpublish_NoArgs(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "unpublish", nil)
	assert.Equal(t, shellContinue, action)
}

// ---------------------------------------------------------------------------
// Commands that call vault operations (exercise error paths)
// ---------------------------------------------------------------------------

func TestShellExec_Mkdir_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Mkdir with no UTXOs will error — exercises the error branch.
	action := shellExecCmd(ctx, "mkdir", []string{"/newdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Put with nonexistent local file → error.
	action := shellExecCmd(ctx, "put", []string{"/nonexistent/file.txt", "/remote.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_WithPrivateAccess(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Create a real local file.
	f := filepath.Join(ctx.localCwd, "test.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello"), 0644))
	// Will fail at vault level (no UTXOs), but exercises the access="private" branch.
	action := shellExecCmd(ctx, "put", []string{f, "/test.txt", "private"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_RelativeLocalPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Create a real local file.
	require.NoError(t, os.WriteFile(filepath.Join(ctx.localCwd, "rel.txt"), []byte("x"), 0644))
	// Relative local path should be joined with localCwd.
	action := shellExecCmd(ctx, "put", []string{"rel.txt", "/rel.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_File(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Remove a file — will fail at vault.Remove (no UTXOs).
	action := shellExecCmd(ctx, "rm", []string{"/hello.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_Recursive(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Recursive remove — exercises the recursive path.
	action := shellExecCmd(ctx, "rm", []string{"-r", "/subdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_RecursiveLongFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "rm", []string{"--recursive", "/subdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mv_SameDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Same-directory move — no cross-dir warning. Fails at vault (no UTXOs).
	action := shellExecCmd(ctx, "mv", []string{"/hello.txt", "/renamed.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cp_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Copy fails at vault (no UTXOs).
	action := shellExecCmd(ctx, "cp", []string{"/hello.txt", "/copy.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Hard link fails at vault (no UTXOs).
	action := shellExecCmd(ctx, "link", []string{"/hello.txt", "/link.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_Soft(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Soft link with -s flag — exercises soft branch.
	action := shellExecCmd(ctx, "link", []string{"/hello.txt", "/slink.txt", "-s"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_SoftLongFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "link", []string{"--soft", "/hello.txt", "/slink.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_ValidPrice(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Sell with valid price — fails at vault (no UTXOs).
	action := shellExecCmd(ctx, "sell", []string{"/hello.txt", "100"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_Recursive(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Recursive sell — exercises shellSellRecursive.
	action := shellExecCmd(ctx, "sell", []string{"/", "100", "--recursive"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_RecursiveShortFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"/", "50", "-r"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_OnlyRecursiveFlag(t *testing.T) {
	ctx := newTestShellCtx(t)
	// "-r" eats one arg, leaving only 1 positional — should print usage
	action := shellExecCmd(ctx, "sell", []string{"-r", "/file"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cat_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Cat nonexistent file → error.
	action := shellExecCmd(ctx, "cat", []string{"/nonexistent"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cat_ExistingFile(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Cat existing file — Cat will try to decrypt, may fail but exercises the path.
	action := shellExecCmd(ctx, "cat", []string{"/hello.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cat_WithForce(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Cat with --force — exercises the force flag branch.
	action := shellExecCmd(ctx, "cat", []string{"/hello.txt", "--force"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Get_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Get nonexistent file → error.
	action := shellExecCmd(ctx, "get", []string{"/nonexistent"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Get_WithLocalPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Get with explicit local path — exercises the localPath branch.
	action := shellExecCmd(ctx, "get", []string{"/hello.txt", "output.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Get_AbsoluteLocalPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "get", []string{"/hello.txt", filepath.Join(ctx.localCwd, "abs.txt")})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mget_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Mget nonexistent remote dir → error.
	action := shellExecCmd(ctx, "mget", []string{"/nonexistent"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mget_WithLocalDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "mget", []string{"/subdir", ctx.localCwd})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mget_RelativeLocalDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	sub := filepath.Join(ctx.localCwd, "dl")
	require.NoError(t, os.MkdirAll(sub, 0755))
	action := shellExecCmd(ctx, "mget", []string{"/subdir", "dl"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Mput nonexistent local dir → error.
	action := shellExecCmd(ctx, "mput", []string{"/nonexistent/dir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_InvalidAccess(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Mput with invalid access mode.
	action := shellExecCmd(ctx, "mput", []string{ctx.localCwd, "/remote", "bogus"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_WithRemoteDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Create a real local dir with a file.
	sub := filepath.Join(ctx.localCwd, "upload")
	require.NoError(t, os.MkdirAll(sub, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0644))
	// Will fail at vault (no UTXOs) but exercises remote dir and access branches.
	action := shellExecCmd(ctx, "mput", []string{sub, "/uploads", "free"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_RelativeLocalDir(t *testing.T) {
	ctx := newTestShellCtx(t)
	sub := filepath.Join(ctx.localCwd, "up")
	require.NoError(t, os.MkdirAll(sub, 0755))
	action := shellExecCmd(ctx, "mput", []string{"up"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Encrypt_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "encrypt", []string{"/hello.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Decrypt_ErrorPath(t *testing.T) {
	ctx := newTestShellCtx(t)
	action := shellExecCmd(ctx, "decrypt", []string{"/hello.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Publish_ListEmpty(t *testing.T) {
	ctx := newTestShellCtx(t)
	// No publish bindings → "No published domains."
	action := shellExecCmd(ctx, "publish", nil)
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Publish_Domain(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Publishing to invalid domain → DNS lookup will fail.
	action := shellExecCmd(ctx, "publish", []string{"invalid.test.domain.xyz"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Unpublish_Domain(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Unpublish a domain that was never published → error.
	action := shellExecCmd(ctx, "unpublish", []string{"nonexistent.test"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sales_NoDaemon(t *testing.T) {
	ctx := newTestShellCtx(t)
	// Sales requires daemon — will fail with connection error.
	action := shellExecCmd(ctx, "sales", nil)
	assert.Equal(t, shellContinue, action)
}

// ---------------------------------------------------------------------------
// Funded shell context for success-path tests
// ---------------------------------------------------------------------------

// newFundedShellCtx creates a shellCtx backed by a vault with root dir,
// fee UTXOs, and a test file — shell operations can succeed.
func newFundedShellCtx(t *testing.T) *shellCtx {
	t.Helper()
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	require.NoError(t, err)
	t.Cleanup(func() { v.Close() })

	return &shellCtx{
		eng:      v,
		vaultIdx: 0,
		cwd:      "/",
		localCwd: t.TempDir(),
	}
}

// ---------------------------------------------------------------------------
// Shell success-path tests (operations that produce TxHex/TxID)
// ---------------------------------------------------------------------------

func TestShellExec_Mkdir_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "mkdir", []string{"/newdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Create a local file in localCwd.
	localFile := filepath.Join(ctx.localCwd, "upload.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("data"), 0644))
	action := shellExecCmd(ctx, "put", []string{"upload.txt", "/upload.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Put_PrivateAccess(t *testing.T) {
	ctx := newFundedShellCtx(t)
	localFile := filepath.Join(ctx.localCwd, "priv.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("secret"), 0644))
	action := shellExecCmd(ctx, "put", []string{"priv.txt", "/priv.txt", "private"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Rm_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "rm", []string{"/test.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mv_SameDir_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "mv", []string{"/test.txt", "/renamed.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cp_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "cp", []string{"/test.txt", "/copy.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Link_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "link", []string{"/test.txt", "/link.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Sell_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"/test.txt", "100"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Encrypt_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "encrypt", []string{"/test.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Cat_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "cat", []string{"/test.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Get_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "get", []string{"/test.txt", filepath.Join(ctx.localCwd, "out.txt")})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mget_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	outDir := filepath.Join(ctx.localCwd, "mget_out")
	action := shellExecCmd(ctx, "mget", []string{"/", outDir})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Mput_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Create local directory structure.
	srcDir := filepath.Join(ctx.localCwd, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644))
	action := shellExecCmd(ctx, "mput", []string{srcDir, "/uploaded"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_Decrypt_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Encrypt first, then decrypt.
	shellExecCmd(ctx, "encrypt", []string{"/test.txt"})
	action := shellExecCmd(ctx, "decrypt", []string{"/test.txt"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_PublishList_Funded(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "publish", []string{"list"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_PublishDomain_Funded(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "publish", []string{"test.example.com"})
	assert.Equal(t, shellContinue, action)
}

// --- Shell recursive operations ---

func TestShellExec_RmRecursive_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Create a nested structure: /rdir/child.txt
	shellExecCmd(ctx, "mkdir", []string{"/rdir"})
	// Create a local file to put.
	f := filepath.Join(ctx.localCwd, "child.txt")
	require.NoError(t, os.WriteFile(f, []byte("data"), 0644))
	shellExecCmd(ctx, "put", []string{f, "/rdir/child.txt"})
	// rm -r should remove directory and contents.
	action := shellExecCmd(ctx, "rm", []string{"-r", "/rdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_SellRecursive_Success(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Create a nested structure: /sdir/item.txt
	shellExecCmd(ctx, "mkdir", []string{"/sdir"})
	f := filepath.Join(ctx.localCwd, "item.txt")
	require.NoError(t, os.WriteFile(f, []byte("content"), 0644))
	shellExecCmd(ctx, "put", []string{f, "/sdir/item.txt"})
	// sell --recursive should apply price to all files.
	action := shellExecCmd(ctx, "sell", []string{"--recursive", "/sdir", "100"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_RmRecursive_NotFound(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "rm", []string{"-r", "/nosuchdir"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_SellRecursive_NotFound(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "sell", []string{"--recursive", "/nosuchdir", "100"})
	assert.Equal(t, shellContinue, action)
}

// --- Shell cat binary path ---

func TestShellExec_CatBinary_Warning(t *testing.T) {
	ctx := newFundedShellCtx(t)
	// Put a binary file.
	binFile := filepath.Join(ctx.localCwd, "img.png")
	require.NoError(t, os.WriteFile(binFile, []byte{0x89, 0x50, 0x4e, 0x47}, 0644))
	shellExecCmd(ctx, "put", []string{binFile, "/img.png"})
	// Cat binary without --force should print warning.
	action := shellExecCmd(ctx, "cat", []string{"/img.png"})
	assert.Equal(t, shellContinue, action)
}

func TestShellExec_CatBinary_Force(t *testing.T) {
	ctx := newFundedShellCtx(t)
	binFile := filepath.Join(ctx.localCwd, "dat.bin")
	require.NoError(t, os.WriteFile(binFile, []byte{0x00, 0x01, 0x02}, 0644))
	shellExecCmd(ctx, "put", []string{binFile, "/dat.bin"})
	action := shellExecCmd(ctx, "cat", []string{"/dat.bin", "--force"})
	assert.Equal(t, shellContinue, action)
}

// --- Shell lcd relative path ---

func TestShellExec_Lcd_Relative(t *testing.T) {
	ctx := newFundedShellCtx(t)
	subDir := filepath.Join(ctx.localCwd, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	action := shellExecCmd(ctx, "lcd", []string{"sub"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, subDir, ctx.localCwd)
}

func TestShellExec_Lcd_BadDir(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "lcd", []string{"/nonexistent_dir_xyz"})
	assert.Equal(t, shellContinue, action)
}

// --- Shell cd to subdirectory ---

func TestShellExec_Cd_Relative(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "cd", []string{"subdir"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/subdir", ctx.cwd)
}

func TestShellExec_Cd_FileNotDir(t *testing.T) {
	ctx := newFundedShellCtx(t)
	action := shellExecCmd(ctx, "cd", []string{"test.txt"})
	assert.Equal(t, shellContinue, action)
	assert.Equal(t, "/", ctx.cwd) // should not change
}

// --- Shell put with access mode ---

func TestShellExec_PutAccess_Private(t *testing.T) {
	ctx := newFundedShellCtx(t)
	f := filepath.Join(ctx.localCwd, "secret.txt")
	require.NoError(t, os.WriteFile(f, []byte("secret"), 0644))
	action := shellExecCmd(ctx, "put", []string{f, "/secret.txt", "private"})
	assert.Equal(t, shellContinue, action)
}

// --- Completer edge cases ---

func TestParseLineForCompletion_EmptyLine(t *testing.T) {
	tokens, current := parseLineForCompletion("")
	assert.Nil(t, tokens)
	assert.Empty(t, current)
}

func TestParseLineForCompletion_TrailingSpace(t *testing.T) {
	tokens, current := parseLineForCompletion("put ")
	assert.Equal(t, []string{"put"}, tokens)
	assert.Empty(t, current)
}

func TestParseLineForCompletion_Partial(t *testing.T) {
	tokens, current := parseLineForCompletion("put /ho")
	assert.Equal(t, []string{"put"}, tokens)
	assert.Equal(t, "/ho", current)
}

func TestFormatCandidates_EmptyInput(t *testing.T) {
	result, _ := formatCandidates(nil, "")
	assert.Nil(t, result)
}

func TestFormatCandidates_WithPrefix(t *testing.T) {
	result, length := formatCandidates([]string{"/home/", "/hello"}, "/h")
	assert.NotEmpty(t, result)
	assert.Equal(t, len([]rune("/h")), length)
}
