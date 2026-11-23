// Package storage checks, before the server starts, that the directories Meerkat was told
// to keep its data in can actually be written to.
//
// Nothing in the container runs as root, so nothing chowns its mounts: when the host
// directories of a bind mount do not exist yet, Docker creates them as root and the
// unprivileged process cannot write in them. Without this check that surfaces as a raw
// SQLite permission error inside a restart loop (docs/getting-started.md).
package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// probePrefix names the throwaway file the writability probe creates.
const probePrefix = ".meerkat-write-check-"

// Probe is one location Meerkat must be able to write to.
type Probe struct {
	// Field is the environment variable that configured the path, named in the message so
	// the operator knows which setting led here.
	Field string
	// HostField is the variable holding the host side of the bind mount (compose passes
	// DATA_PATH and PHOTOS_PATH in through .env). Quoted in the fix when it is set.
	HostField string
	// Dir is the directory that must be writable; created when it is missing.
	Dir string
	// File optionally names a file that must be openable for writing too — the database,
	// which can exist and belong to another user while its directory looks fine.
	File string
}

// Problem is a Probe location Meerkat could not write to.
type Problem struct {
	Probe
	Path string // the directory or the file the failure is about
	Err  error
	UID  int
	GID  int
}

// CheckWritable probes each location and returns one Problem per path Meerkat cannot write
// to. Writability means creating and deleting a file there.
func CheckWritable(probes ...Probe) []Problem {
	uid, gid := os.Getuid(), os.Getgid()
	var problems []Problem
	for _, p := range probes {
		report := func(path string, err error) {
			if err != nil {
				problems = append(problems, Problem{Probe: p, Path: path, Err: err, UID: uid, GID: gid})
			}
		}
		if p.Dir == "" {
			continue
		}
		// Photo uploads and a first database both create their files on demand, so a missing
		// directory is made here rather than declared unusable.
		if err := os.MkdirAll(p.Dir, 0o755); err != nil {
			report(p.Dir, err)
			continue
		}
		if err := writableDir(p.Dir); err != nil {
			report(p.Dir, err)
			continue
		}
		if p.File != "" {
			report(p.File, writableFile(p.File))
		}
	}
	return problems
}

// writableDir reports whether the running user may create and delete a file in dir.
func writableDir(dir string) error {
	f, err := os.CreateTemp(dir, probePrefix+"*")
	if err != nil {
		return err
	}
	// A file that cannot be removed means the directory is not usable either (a remount that
	// went read-only, say), and leaving probe files behind is not acceptable.
	closeErr := f.Close()
	return errors.Join(closeErr, os.Remove(f.Name()))
}

// writableFile reports whether an existing file can be opened for writing. A file that is
// not there yet is fine: the database and the photos are created on demand. O_WRONLY
// without O_TRUNC, so this cannot damage the database.
func writableFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// Error is what the operator reads: the path, who Meerkat is running as, and the command
// that fixes the ownership case this check exists for.
func (p Problem) Error() string {
	head := fmt.Sprintf("%s (%s) is not writable by the user Meerkat runs as (uid %d, gid %d): %v",
		p.Path, p.Field, p.UID, p.GID, p.Err)
	if !errors.Is(p.Err, fs.ErrPermission) {
		// Not the ownership case: a read-only remount, a file where a directory should be.
		// Telling someone to chown those wastes their time.
		return head + "\n    Check the mount: the database with its -wal and -shm files and the photo files live there."
	}
	// The host path is not knowable from inside the container. compose passes it in as
	// DATA_PATH / PHOTOS_PATH, so name it when the container can see them.
	host := os.Getenv(p.HostField)
	named := host != ""
	if !named {
		host = fmt.Sprintf("<the host directory mounted at %s>", p.Dir)
	}
	msg := fmt.Sprintf("%s\n"+
		"    Nothing in the container runs as root, so nothing chowns the mount. On the host:\n"+
		"        chown -R %d:%d %s", head, p.UID, p.GID, host)
	if named {
		msg += fmt.Sprintf("\n    %s is the path mounted at %s (docs/getting-started.md).", p.HostField, p.Dir)
	}
	return msg
}
