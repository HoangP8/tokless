package util

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DownloadAndExtractTarGz fetches a tar.gz and unpacks it as-is into dest.
func DownloadAndExtractTarGz(url, dest string) error {
	tmp, err := downloadToTemp(url)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return ExtractTarGzFlat(tmp, dest)
}

func nodeLinuxArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	}
	return ""
}

func nodeUnixInstallDir() string {
	return filepath.Join(Home(), ".local", "share", "tokless", "node")
}

// installNodeUnixTarball: official tar.gz, pure-Go extract, bins symlinked
// into ~/.local/bin — no fnm/unzip/sudo, system node untouched.
func installNodeUnixTarball() bool {
	arch := nodeLinuxArch()
	if arch == "" {
		L.Err("unsupported architecture for Node install: " + runtime.GOARCH)
		return false
	}
	entries, ok := fetchNodeIndex()
	if !ok {
		L.Err("couldn't fetch the Node.js release index")
		return false
	}
	v, ok := nodeLTSVersion(entries, "linux-"+arch)
	if !ok {
		L.Err("no Node LTS tarball for linux-" + arch)
		return false
	}
	L.Info("Downloading Node.js " + v + " from nodejs.org…")
	name := "node-" + v + "-linux-" + arch
	tarPath, err := downloadToTemp("https://nodejs.org/dist/" + v + "/" + name + ".tar.gz")
	if err != nil {
		L.Err("Node download failed: " + err.Error())
		return false
	}
	defer os.Remove(tarPath)

	dest := nodeUnixInstallDir()
	_ = os.RemoveAll(dest)
	if err := extractTarGzStripRoot(tarPath, dest); err != nil {
		L.Err("Node extract failed: " + err.Error())
		return false
	}

	localBin := filepath.Join(Home(), ".local", "bin")
	_ = os.MkdirAll(localBin, 0o755)
	for _, b := range []string{"node", "npm", "npx"} {
		link := filepath.Join(localBin, b)
		_ = os.Remove(link)
		if err := os.Symlink(filepath.Join(dest, "bin", b), link); err != nil {
			L.Err("couldn't link " + b + " into " + localBin + ": " + err.Error())
			return false
		}
	}
	PrependProcessPath(localBin)
	_ = Run(filepath.Join(localBin, "npm"), []string{"config", "set", "prefix", filepath.Join(Home(), ".local")}, RunOptions{})
	return Which("node") != "" && Which("npm") != ""
}

func extractTarGzStripRoot(path, dest string) error { return extractTarGz(path, dest, true) }

func ExtractTarGzFlat(path, dest string) error { return extractTarGz(path, dest, false) }

func extractTarGz(path, dest string, stripRoot bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(filepath.ToSlash(hdr.Name), "./")
		if stripRoot {
			idx := strings.IndexByte(rel, '/')
			if idx < 0 {
				continue
			}
			rel = rel[idx+1:]
		}
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			resolved := filepath.Join(filepath.Dir(target), filepath.FromSlash(hdr.Linkname))
			if r, err := filepath.Rel(dest, resolved); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(filepath.FromSlash(hdr.Linkname), target); err != nil {
				return err
			}
		}
	}
}
