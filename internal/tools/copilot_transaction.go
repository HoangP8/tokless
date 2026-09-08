package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

var copilotTransactionMu sync.Mutex

type copilotFileSnapshot struct {
	path    string
	data    []byte
	exists  bool
	mode    os.FileMode
	symlink string
}

func copilotTransactionPaths() []string {
	p := util.CopilotPathsResolved()
	root, err := filepath.Abs(agents.IdeProjectRoot())
	if err != nil {
		root = filepath.Clean(agents.IdeProjectRoot())
	}
	return []string{
		p.McpConfig,
		agents.CopilotShimDigestPath(),
		filepath.Join(p.HooksDir, "context-mode.json"),
		filepath.Join(p.HooksDir, "tokless-codegraph-index.json"),
		filepath.Join(p.HooksDir, "tokless-rtk.json"),
		p.Instructions,
		filepath.Join(p.Dir, "permissions-config.json"),
		filepath.Join(root, ".vscode", "mcp.json"),
		filepath.Join(root, ".github", "hooks", "context-mode.json"),
		filepath.Join(root, ".github", "hooks", "tokless-codegraph-index.json"),
		filepath.Join(root, ".github", "hooks", "tokless-rtk.json"),
		filepath.Join(root, ".github", "copilot-instructions.md"),
	}
}

func withCopilotTransaction(fn func() error) error {
	copilotTransactionMu.Lock()
	defer copilotTransactionMu.Unlock()
	snapshots := make([]copilotFileSnapshot, 0, len(copilotTransactionPaths()))
	for _, path := range copilotTransactionPaths() {
		root, _ := filepath.Abs(agents.IdeProjectRoot())
		allowSymlink := root != "" && filepath.HasPrefix(filepath.Clean(path), root+string(os.PathSeparator))
		if err := rejectCopilotSymlinkPath(path, allowSymlink); err != nil {
			return err
		}
		snapshot := copilotFileSnapshot{path: path, mode: 0o644}
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if !allowSymlink {
					return fmt.Errorf("refusing Copilot transaction symlink %s", path)
				}
				snapshot.symlink, err = os.Readlink(path)
				if err != nil {
					return err
				}
				info, err = os.Stat(path)
				if err != nil {
					return err
				}
			}
			snapshot.data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot.exists = true
			snapshot.mode = info.Mode().Perm()
		} else if !os.IsNotExist(err) {
			return err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := fn(); err == nil {
		return nil
	} else {
		cause := err
		var rollbackErrs []error
		for _, snapshot := range snapshots {
			if snapshot.exists {
				if restoreErr := restoreCopilotSnapshot(snapshot); restoreErr != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: %w", snapshot.path, restoreErr))
				}
				continue
			}
			if err := rejectCopilotSymlinkPath(snapshot.path, false); err != nil {
				rollbackErrs = append(rollbackErrs, err)
				continue
			}
			if info, statErr := os.Lstat(snapshot.path); statErr == nil {
				if !info.Mode().IsRegular() || !agents.CopilotTransactionFileOwned(snapshot.path) {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: path changed or is foreign", snapshot.path))
					continue
				}
				if removeErr := os.Remove(snapshot.path); removeErr != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: %w", snapshot.path, removeErr))
				}
			} else if !os.IsNotExist(statErr) {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: %w", snapshot.path, statErr))
			}
		}
		return errors.Join(cause, errors.Join(rollbackErrs...))
	}
}

func rejectCopilotSymlinkPath(path string, allowLeaf bool) error {
	clean := filepath.Clean(path)
	for current := filepath.Clean(path); current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if allowLeaf && current == clean {
				continue
			}
			return fmt.Errorf("refusing Copilot transaction symlink %s", current)
		}
	}
	return nil
}

func restoreCopilotSnapshot(snapshot copilotFileSnapshot) error {
	if err := rejectCopilotSymlinkPath(snapshot.path, snapshot.symlink != ""); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o755); err != nil {
		return err
	}
	if err := rejectCopilotSymlinkPath(snapshot.path, snapshot.symlink != ""); err != nil {
		return err
	}
	target := snapshot.path
	if snapshot.symlink != "" {
		link, err := os.Readlink(snapshot.path)
		if err != nil || link != snapshot.symlink {
			return fmt.Errorf("Copilot transaction symlink changed during rollback %s", snapshot.path)
		}
		target = snapshot.symlink
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(snapshot.path), target)
		}
	}
	if snapshot.symlink != "" {
		link, err := os.Readlink(snapshot.path)
		if err != nil || link != snapshot.symlink {
			return fmt.Errorf("Copilot transaction symlink changed during rollback %s", snapshot.path)
		}
	}
	if err := util.WriteFileAtomic(target, string(snapshot.data), snapshot.mode); err != nil {
		return err
	}
	if snapshot.symlink != "" {
		link, err := os.Readlink(snapshot.path)
		if err != nil || link != snapshot.symlink {
			return fmt.Errorf("Copilot transaction symlink changed during rollback %s", snapshot.path)
		}
	}
	return nil
}
