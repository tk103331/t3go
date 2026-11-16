package provider

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/proto"
)

const acpRegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

type acpRegistryDocument struct {
	Version string                   `json:"version"`
	Agents  []proto.ACPRegistryAgent `json:"agents"`
}

var acpRegistryMu sync.Mutex

func loadACPCatalog(ctx context.Context, home string, force bool) (acpRegistryDocument, error) {
	acpRegistryMu.Lock()
	defer acpRegistryMu.Unlock()
	cachePath := filepath.Join(home, "cache", "acp-registry", "registry.json")
	if !force {
		if st, err := os.Stat(cachePath); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
			if document, err := readACPCatalog(cachePath); err == nil {
				return document, nil
			}
		}
	}
	document, err := fetchACPCatalog(ctx)
	if err != nil {
		if cached, cacheErr := readACPCatalog(cachePath); cacheErr == nil {
			return cached, nil
		}
		return acpRegistryDocument{}, err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err == nil {
		if raw, marshalErr := json.Marshal(document); marshalErr == nil {
			_ = os.WriteFile(cachePath, raw, 0o600)
		}
	}
	return document, nil
}

func fetchACPCatalog(ctx context.Context) (acpRegistryDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, acpRegistryURL, nil)
	if err != nil {
		return acpRegistryDocument{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return acpRegistryDocument{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return acpRegistryDocument{}, fmt.Errorf("ACP Registry returned %s", resp.Status)
	}
	var document acpRegistryDocument
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(&document); err != nil {
		return acpRegistryDocument{}, err
	}
	return document, nil
}

func readACPCatalog(path string) (acpRegistryDocument, error) {
	var document acpRegistryDocument
	raw, err := os.ReadFile(path)
	if err != nil {
		return document, err
	}
	err = json.Unmarshal(raw, &document)
	return document, err
}

func searchACPCatalog(ctx context.Context, home, query string, force bool) ([]proto.ACPRegistryAgent, error) {
	document, err := loadACPCatalog(ctx, home, force)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	agents := make([]proto.ACPRegistryAgent, 0, len(document.Agents))
	for _, agent := range document.Agents {
		agent.Distributions = compatibleDistributionNames(agent)
		if len(agent.Distributions) == 0 {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(agent.ID + " " + agent.Name + " " + agent.Description)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		agents = append(agents, agent)
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if strings.EqualFold(agents[i].Name, query) {
			return true
		}
		return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
	})
	return agents, nil
}

// ACPRegistrySearch lists compatible registry agents visible to clients.
func ACPRegistrySearch(ctx context.Context, home, query string, refresh bool) ([]proto.ACPRegistryAgent, error) {
	return searchACPCatalog(ctx, home, query, refresh)
}

func compatibleDistributionNames(agent proto.ACPRegistryAgent) []string {
	var names []string
	if agent.Distribution.NPX != nil && pinnedPackage(agent.Distribution.NPX.Package, agent.Version, false) {
		names = append(names, "npx")
	}
	if agent.Distribution.UVX != nil && pinnedPackage(agent.Distribution.UVX.Package, agent.Version, true) {
		names = append(names, "uvx")
	}
	if _, ok := binaryForPlatform(agent.Distribution.Binary); ok {
		names = append(names, "binary")
	}
	return names
}

func pinnedPackage(pkg, version string, python bool) bool {
	pkg, version = strings.TrimSpace(pkg), strings.TrimSpace(version)
	if pkg == "" || version == "" {
		return false
	}
	if python {
		return strings.HasSuffix(pkg, "=="+version) || strings.HasSuffix(pkg, "@"+version)
	}
	return strings.HasSuffix(pkg, "@"+version)
}

func acpRegistryAgent(ctx context.Context, home, agentID string) (proto.ACPRegistryAgent, error) {
	document, err := loadACPCatalog(ctx, home, false)
	if err != nil {
		return proto.ACPRegistryAgent{}, err
	}
	for _, agent := range document.Agents {
		if agent.ID == agentID {
			agent.Distributions = compatibleDistributionNames(agent)
			return agent, nil
		}
	}
	return proto.ACPRegistryAgent{}, fmt.Errorf("ACP Registry does not contain agent %q", agentID)
}

func resolveACPCommand(ctx context.Context, cfg proto.ACPProviderSettings, home string) (procSpec, string, error) {
	if cfg.Source == "local" {
		path, err := resolveACPPath(cfg.CommandPath)
		if err != nil {
			return procSpec{}, "", err
		}
		return procSpec{Path: path, Args: append([]string(nil), cfg.CommandArgs...)}, "", nil
	}
	agent, err := acpRegistryAgent(ctx, home, cfg.AgentID)
	if err != nil {
		return procSpec{}, "", err
	}
	distribution := strings.TrimSpace(cfg.Distribution)
	if distribution == "" || distribution == "auto" {
		switch {
		case agent.Distribution.NPX != nil:
			distribution = "npx"
		case agent.Distribution.UVX != nil:
			distribution = "uvx"
		case len(agent.Distribution.Binary) > 0:
			distribution = "binary"
		}
	}
	switch distribution {
	case "npx":
		if agent.Distribution.NPX == nil {
			return procSpec{}, "", errors.New("this ACP agent has no npx distribution")
		}
		if !pinnedPackage(agent.Distribution.NPX.Package, agent.Version, false) {
			return procSpec{}, "", errors.New("ACP Registry npx package is not pinned to the agent version")
		}
		path, err := lookPath("npx")
		if err != nil {
			return procSpec{}, "", errors.New("npx is not installed on this environment")
		}
		args := []string{"-y", agent.Distribution.NPX.Package}
		args = append(args, agent.Distribution.NPX.Args...)
		args = append(args, cfg.CommandArgs...)
		return procSpec{Path: path, Args: args, Env: mergeEnv(providerEnvironment(), agent.Distribution.NPX.Env)}, agent.Version, nil
	case "uvx":
		if agent.Distribution.UVX == nil {
			return procSpec{}, "", errors.New("this ACP agent has no uvx distribution")
		}
		if !pinnedPackage(agent.Distribution.UVX.Package, agent.Version, true) {
			return procSpec{}, "", errors.New("ACP Registry uvx package is not pinned to the agent version")
		}
		path, err := lookPath("uvx")
		if err != nil {
			return procSpec{}, "", errors.New("uvx is not installed on this environment")
		}
		args := []string{agent.Distribution.UVX.Package}
		args = append(args, agent.Distribution.UVX.Args...)
		args = append(args, cfg.CommandArgs...)
		return procSpec{Path: path, Args: args, Env: mergeEnv(providerEnvironment(), agent.Distribution.UVX.Env)}, agent.Version, nil
	case "binary":
		binary, ok := binaryForPlatform(agent.Distribution.Binary)
		if !ok {
			return procSpec{}, "", fmt.Errorf("ACP agent %q has no binary for %s/%s", agent.ID, runtime.GOOS, runtime.GOARCH)
		}
		path, err := installedACPPath(home, agent, binary)
		if err != nil {
			return procSpec{}, agent.Version, fmt.Errorf("registry agent is not prepared: %w", err)
		}
		args := append([]string(nil), binary.Args...)
		args = append(args, cfg.CommandArgs...)
		return procSpec{Path: path, Args: args, Env: mergeEnv(providerEnvironment(), binary.Env)}, agent.Version, nil
	default:
		return procSpec{}, "", fmt.Errorf("unknown ACP distribution %q", distribution)
	}
}

func resolveACPPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("ACP executable is required")
	}
	if strings.ContainsAny(path, `/\\`) {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return "", fmt.Errorf("ACP executable %q is not available", path)
		}
		return path, nil
	}
	return lookPath(path)
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := append([]string(nil), base...)
	for key, value := range overrides {
		prefix := key + "="
		filtered := env[:0]
		for _, entry := range env {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		env = append(filtered, prefix+value)
	}
	return env
}

func binaryForPlatform(binaries map[string]proto.ACPBinaryDistribution) (proto.ACPBinaryDistribution, bool) {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	key := runtime.GOOS + "-" + arch
	binary, ok := binaries[key]
	return binary, ok
}

func installedACPPath(home string, agent proto.ACPRegistryAgent, binary proto.ACPBinaryDistribution) (string, error) {
	if !safePathComponent(agent.ID) || !safePathComponent(agent.Version) {
		return "", errors.New("registry agent has an unsafe installation path")
	}
	cmd := strings.TrimPrefix(filepath.Clean(strings.ReplaceAll(binary.CMD, "\\", "/")), "./")
	root := filepath.Join(home, "tools", agent.ID, agent.Version)
	path := filepath.Join(root, cmd)
	if !withinRoot(root, path) {
		return "", errors.New("registry command escapes the installation directory")
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", errors.New("select Prepare in provider settings")
	}
	return path, nil
}

func safePathComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`) &&
		!strings.ContainsRune(value, filepath.Separator)
}

func prepareACPAgent(ctx context.Context, home, agentID string) (string, error) {
	agent, err := acpRegistryAgent(ctx, home, agentID)
	if err != nil {
		return "", err
	}
	if !safePathComponent(agent.ID) || !safePathComponent(agent.Version) {
		return "", errors.New("registry agent has an unsafe installation path")
	}
	binary, ok := binaryForPlatform(agent.Distribution.Binary)
	if !ok {
		return "", fmt.Errorf("ACP agent %q has no binary for %s/%s", agent.ID, runtime.GOOS, runtime.GOARCH)
	}
	if path, err := installedACPPath(home, agent, binary); err == nil {
		return path, nil
	}
	archiveURL, err := url.Parse(binary.Archive)
	if err != nil || archiveURL.Scheme != "https" || archiveURL.Host == "" {
		return "", errors.New("registry archive must use HTTPS")
	}
	root := filepath.Join(home, "tools", agent.ID, agent.Version)
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	archivePath := filepath.Join(stage, "archive")
	if err := downloadVerified(ctx, binary.Archive, binary.SHA256, archivePath); err != nil {
		return "", err
	}
	extractRoot := filepath.Join(stage, "contents")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		return "", err
	}
	if err := extractACPArchive(archivePath, extractRoot); err != nil {
		return "", err
	}
	cmd := strings.TrimPrefix(filepath.Clean(strings.ReplaceAll(binary.CMD, "\\", "/")), "./")
	commandPath := filepath.Join(extractRoot, cmd)
	if !withinRoot(extractRoot, commandPath) {
		return "", errors.New("registry archive command escapes the installation directory")
	}
	if err := os.Chmod(commandPath, 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(extractRoot, root); err != nil {
		if _, statErr := os.Stat(filepath.Join(root, cmd)); statErr != nil {
			return "", err
		}
	}
	return filepath.Join(root, cmd), nil
}

// PrepareACPRegistryAgent downloads and verifies a binary distribution.
func PrepareACPRegistryAgent(ctx context.Context, home, agentID string) (string, error) {
	return prepareACPAgent(ctx, home, agentID)
}

func downloadVerified(ctx context.Context, address, expectedSHA, destination string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	if final := resp.Request.URL; final.Scheme != "https" {
		return errors.New("registry download redirected away from HTTPS")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 300<<20))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 300<<20 {
		return errors.New("registry archive exceeds the 300 MB limit")
	}
	if expectedSHA != "" {
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(got, expectedSHA) {
			return fmt.Errorf("registry archive checksum mismatch: got %s", got)
		}
	}
	return nil
}

func extractACPArchive(archivePath, destination string) error {
	raw, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	if len(raw) >= 2 && raw[0] == 'P' && raw[1] == 'K' {
		zr, err := zip.NewReader(strings.NewReader(string(raw)), int64(len(raw)))
		if err != nil {
			return err
		}
		for _, file := range zr.File {
			target := filepath.Join(destination, file.Name)
			if !withinRoot(destination, target) {
				return errors.New("archive contains an unsafe path")
			}
			if file.FileInfo().IsDir() {
				if err := os.MkdirAll(target, 0o700); err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			src, err := file.Open()
			if err != nil {
				return err
			}
			dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				src.Close()
				return err
			}
			_, copyErr := io.Copy(dst, io.LimitReader(src, 300<<20))
			src.Close()
			closeErr := dst.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	}
	gz, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		return errors.New("unsupported registry archive; expected zip or tar.gz")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, header.Name)
		if !withinRoot(destination, target) {
			return errors.New("archive contains an unsafe path")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(dst, io.LimitReader(tr, 300<<20))
			closeErr := dst.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
