package onek

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	buildCacheVersion = 2
	buildCacheRacy    = 2 * time.Second
	cacheDirEnv       = "ONKT_CACHE_DIR"
	noCacheEnv        = "ONKT_NO_CACHE"
)

type cachedOutput struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	Digest string `json:"digest"`
}

type buildCacheEntry struct {
	Version     int               `json:"version"`
	Fingerprint string            `json:"fingerprint"`
	ExeSize     int64             `json:"exe_size"`
	ExeMTime    int64             `json:"exe_mtime"`
	ExeHash     string            `json:"exe_hash"`
	SavedAt     int64             `json:"saved_at"`
	Files       int               `json:"files"`
	Outputs     []cachedOutput    `json:"outputs"`
	ConfigHash  string            `json:"config_hash,omitempty"`
	GroupSet    string            `json:"group_set,omitempty"`
	Groups      map[string]string `json:"groups,omitempty"`
}

var outputRecorder struct {
	sync.Mutex
	active  bool
	digests map[string][sha256.Size]byte
}

func startRecordingOutputs() {
	outputRecorder.Lock()
	outputRecorder.active = true
	outputRecorder.digests = map[string][sha256.Size]byte{}
	outputRecorder.Unlock()
}

func stopRecordingOutputs() map[string][sha256.Size]byte {
	outputRecorder.Lock()
	defer outputRecorder.Unlock()
	digests := outputRecorder.digests
	outputRecorder.active = false
	outputRecorder.digests = nil
	return digests
}

func recordOutput(path string, data []byte) {
	outputRecorder.Lock()
	active := outputRecorder.active
	outputRecorder.Unlock()
	if !active {
		return
	}
	digest := sha256.Sum256(data)
	outputRecorder.Lock()
	if outputRecorder.active {
		outputRecorder.digests[path] = digest
	}
	outputRecorder.Unlock()
}

func buildCachePath(projectDir string) (string, bool) {
	if os.Getenv(noCacheEnv) != "" {
		return "", false
	}
	root := os.Getenv(cacheDirEnv)
	if root == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", false
		}
		root = filepath.Join(base, "onekit")
	}
	project, err := filepath.Abs(projectDir)
	if err != nil {
		return "", false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(project); resolveErr == nil {
		project = resolved
	}
	sum := sha256.Sum256([]byte(project))
	return filepath.Join(root, "builds", hex.EncodeToString(sum[:16])+".json"), true
}

type exeStamp struct {
	size  int64
	mtime int64
}

func executableIdentity() (exeStamp, bool) {
	exe, err := os.Executable()
	if err != nil {
		return exeStamp{}, false
	}
	info, err := os.Stat(exe)
	if err != nil {
		return exeStamp{}, false
	}
	return exeStamp{size: info.Size(), mtime: info.ModTime().UnixNano()}, true
}

func executableHash() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

func sameExecutable(entry *buildCacheEntry) bool {
	stamp, ok := executableIdentity()
	if !ok || entry.ExeHash == "" {
		return false
	}
	if stamp.size == entry.ExeSize && stamp.mtime == entry.ExeMTime {
		return true
	}
	hash, ok := executableHash()
	return ok && hash == entry.ExeHash
}

type schemaFingerprint struct {
	sum        string
	files      map[string][sha256.Size]byte
	configHash string
}

func buildFingerprint(cfg *Config) (schemaFingerprint, error) {
	paths, err := discoverSchemaFiles(cfg.SchemaDir())
	if err != nil {
		return schemaFingerprint{}, err
	}
	sort.Strings(paths)
	sums := make([][sha256.Size]byte, len(paths))
	errs := make([]error, len(paths))
	parallelFor(len(paths), func(i int) {
		data, readErr := readRegularFile(paths[i])
		if readErr != nil {
			errs[i] = readErr
			return
		}
		sums[i] = sha256.Sum256(data)
	})
	for _, readErr := range errs {
		if readErr != nil {
			return schemaFingerprint{}, readErr
		}
	}
	configData, err := readRegularFile(configFilePath(cfg.dir))
	if err != nil {
		return schemaFingerprint{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte{buildCacheVersion})
	for i, path := range paths {
		rel, relErr := filepath.Rel(cfg.SchemaDir(), path)
		if relErr != nil {
			return schemaFingerprint{}, relErr
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(sums[i][:])
	}
	_, _ = hash.Write([]byte{1})
	_, _ = hash.Write(configData)
	files := make(map[string][sha256.Size]byte, len(paths))
	for i, path := range paths {
		files[filepath.Clean(path)] = sums[i]
	}
	configSum := sha256.Sum256(configData)
	return schemaFingerprint{sum: hex.EncodeToString(hash.Sum(nil)), files: files, configHash: hex.EncodeToString(configSum[:])}, nil
}

func loadBuildCache(path string) *buildCacheEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry buildCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Version != buildCacheVersion {
		return nil
	}
	return &entry
}

func outputsUnchanged(projectDir string, entry *buildCacheEntry) bool {
	if len(entry.Outputs) == 0 {
		return false
	}
	savedAt := time.Unix(0, entry.SavedAt)
	var stale atomic.Bool
	parallelFor(len(entry.Outputs), func(i int) {
		if stale.Load() {
			return
		}
		output := entry.Outputs[i]
		path := filepath.Join(projectDir, filepath.FromSlash(output.Path))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != output.Size || info.ModTime().UnixNano() != output.MTime {
			stale.Store(true)
			return
		}
		if savedAt.Sub(info.ModTime()) > buildCacheRacy {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			stale.Store(true)
			return
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != output.Digest {
			stale.Store(true)
		}
	})
	return !stale.Load()
}

// tryCachedBuild reports whether the previous build is still exact. It also
// returns the previous entry when it was made by this same onkt binary, so a
// partial rebuild can reuse the directories that did not change.
func tryCachedBuild(cfg *Config, fingerprint string) (bool, *buildCacheEntry) {
	cachePath, ok := buildCachePath(cfg.dir)
	if !ok {
		return false, nil
	}
	entry := loadBuildCache(cachePath)
	if entry == nil || !sameExecutable(entry) {
		return false, nil
	}
	if entry.Fingerprint != fingerprint || !outputsUnchanged(cfg.dir, entry) {
		return false, entry
	}
	writtenFiles.Add(int64(entry.Files))
	return true, entry
}

func saveBuildCache(cfg *Config, fingerprint schemaFingerprint, exeHash string, digests map[string][sha256.Size]byte, files int, groupSet string, groups map[string]string) {
	cachePath, ok := buildCachePath(cfg.dir)
	if !ok {
		return
	}
	stamp, ok := executableIdentity()
	if !ok {
		return
	}
	if exeHash == "" {
		return
	}
	paths := make([]string, 0, len(digests))
	for path := range digests {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	outputs := make([]cachedOutput, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(cfg.dir, path)
		if err != nil {
			return
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		digest := digests[path]
		outputs = append(outputs, cachedOutput{
			Path:   filepath.ToSlash(rel),
			Size:   info.Size(),
			MTime:  info.ModTime().UnixNano(),
			Digest: hex.EncodeToString(digest[:]),
		})
	}
	entry := buildCacheEntry{
		Version:     buildCacheVersion,
		Fingerprint: fingerprint.sum,
		ConfigHash:  fingerprint.configHash,
		GroupSet:    groupSet,
		Groups:      groups,
		ExeSize:     stamp.size,
		ExeMTime:    stamp.mtime,
		ExeHash:     exeHash,
		SavedAt:     time.Now().UnixNano(),
		Files:       files,
		Outputs:     outputs,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".build-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil || os.Rename(tmpPath, cachePath) != nil {
		_ = os.Remove(tmpPath)
	}
}
