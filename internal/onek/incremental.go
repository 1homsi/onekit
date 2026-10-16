package onek

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/1homsi/onekit/internal/onkir"
)

// incrementalPlan lists the schema directories whose generated output can be
// kept from the previous build, because nothing they are generated from has
// changed: their own schema files, the schema files of every directory whose
// types they reference, the project's set of directories, onekit.toml and the
// onek binary. The per-directory targets (Go, TypeScript, Python, OpenAPI)
// skip those directories; every other target regenerates everything.
type incrementalPlan struct {
	clean  map[string]bool
	reused map[string][sha256.Size]byte
}

var lastCleanGroups atomic.Int64

// eachChangedGroup is eachGroup for a target whose output for one directory
// depends only on that directory's fingerprint: it skips the directories the
// build keeps from the previous one.
func eachChangedGroup(idx *sourceIndex, fn func(*sourceGroup) error) error {
	if len(idx.unchanged) == 0 {
		return eachGroup(idx, fn)
	}
	changed := *idx
	changed.groups = nil
	for _, g := range idx.groups {
		if !idx.unchanged[g.relDir] {
			changed.groups = append(changed.groups, g)
		}
	}
	return eachGroup(&changed, fn)
}

// groupFingerprints hashes, for every directory, what its per-directory
// output is generated from. ok is false when a compiled file cannot be
// matched to the schema file it came from; the build then regenerates
// everything.
func groupFingerprints(idx *sourceIndex, pkg *onkir.Package, schemaRoot string, fileSums map[string][sha256.Size]byte) (map[string]string, string, bool) {
	own := map[string]*hashList{}
	for _, f := range pkg.Files {
		rel, err := relDirOf(schemaRoot, f)
		if err != nil {
			return nil, "", false
		}
		sum, found := fileSums[filepath.Clean(f.Path)]
		if !found {
			return nil, "", false
		}
		if own[rel] == nil {
			own[rel] = &hashList{}
		}
		own[rel].add(filepath.Base(f.Path), sum[:])
	}
	ownHash := map[string]string{}
	dirs := make([]string, 0, len(idx.groups))
	for _, g := range idx.groups {
		list := own[g.relDir]
		if list == nil {
			return nil, "", false
		}
		ownHash[g.relDir] = list.sum()
		dirs = append(dirs, g.relDir)
	}
	sort.Strings(dirs)
	set := &hashList{}
	for _, dir := range dirs {
		set.add(dir, nil)
	}
	groupSet := set.sum()
	out := make(map[string]string, len(idx.groups))
	for _, g := range idx.groups {
		deps, ok := groupDependencies(idx, g)
		if !ok {
			return nil, "", false
		}
		h := &hashList{}
		h.add("set", []byte(groupSet))
		h.add("own", []byte(ownHash[g.relDir]))
		for _, dep := range deps {
			h.add(dep, []byte(ownHash[dep]))
		}
		out[g.relDir] = h.sum()
	}
	return out, groupSet, true
}

// groupDependencies lists the other directories whose types the directory's
// messages, enums and methods reach, following message fields transitively.
func groupDependencies(idx *sourceIndex, g *sourceGroup) ([]string, bool) {
	dirs := map[string]bool{}
	seen := map[*onkir.Message]bool{}
	ok := true
	var visitMessage func(m *onkir.Message)
	var visitType func(t *onkir.Type)
	addEnum := func(e *onkir.Enum) {
		dir, found := idx.dirByEnum[e]
		if !found {
			ok = false
			return
		}
		dirs[dir] = true
	}
	visitType = func(t *onkir.Type) {
		for t != nil {
			switch t.Kind {
			case onkir.KindMessage:
				visitMessage(t.Message)
				return
			case onkir.KindEnum:
				if t.Enum != nil {
					addEnum(t.Enum)
				}
				return
			case onkir.KindMap:
				t = t.MapValue
			default:
				return
			}
		}
	}
	visitMessage = func(m *onkir.Message) {
		if m == nil || seen[m] {
			return
		}
		seen[m] = true
		dir, found := idx.dirByMessage[m]
		if !found {
			ok = false
			return
		}
		dirs[dir] = true
		for _, f := range m.Fields {
			visitType(f.Type)
			if f.Oneof != nil {
				for _, v := range f.Oneof.Variants {
					visitType(v.Type)
				}
			}
		}
		for _, nested := range m.Nested {
			visitMessage(nested)
		}
		for _, e := range m.NestedEnums {
			addEnum(e)
		}
	}
	for _, m := range g.file.Messages {
		visitMessage(m)
	}
	for _, e := range g.file.Enums {
		addEnum(e)
	}
	for _, s := range g.file.Services {
		for _, method := range s.Methods {
			visitMessage(method.Request)
			visitMessage(method.Response)
			visitMessage(method.Principal)
			for _, e := range method.ErrorTypes {
				visitMessage(e)
			}
		}
	}
	delete(dirs, g.relDir)
	out := make([]string, 0, len(dirs))
	for dir := range dirs {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out, ok
}

type hashList struct {
	parts [][2][]byte
}

func (h *hashList) add(key string, value []byte) {
	h.parts = append(h.parts, [2][]byte{[]byte(key), append([]byte(nil), value...)})
}

func (h *hashList) sum() string {
	sort.Slice(h.parts, func(i, j int) bool { return string(h.parts[i][0]) < string(h.parts[j][0]) })
	sum := sha256.New()
	for _, part := range h.parts {
		_, _ = sum.Write(part[0])
		_, _ = sum.Write([]byte{0})
		_, _ = sum.Write(part[1])
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// planIncremental decides which directories keep their output. A directory
// is kept only when its fingerprint matches the previous build and every
// per-directory output it is expected to have is still on disk exactly as the
// previous build left it (or was absent then and is absent now).
func planIncremental(cfg *Config, idx *sourceIndex, prev *buildCacheEntry, fingerprints map[string]string, groupSet, configHash string) *incrementalPlan {
	if prev == nil || prev.ConfigHash != configHash || prev.GroupSet != groupSet || len(prev.Groups) == 0 {
		return nil
	}
	previous := make(map[string]cachedOutput, len(prev.Outputs))
	for _, output := range prev.Outputs {
		previous[filepath.Join(cfg.dir, filepath.FromSlash(output.Path))] = output
	}
	savedAt := time.Unix(0, prev.SavedAt)
	plan := &incrementalPlan{clean: map[string]bool{}, reused: map[string][sha256.Size]byte{}}
	expected := expectedGroupOutputs(cfg, idx)
	for _, g := range idx.groups {
		if fingerprints[g.relDir] == "" || prev.Groups[g.relDir] != fingerprints[g.relDir] {
			continue
		}
		reused, ok := verifyPreviousOutputs(expected[g.relDir], previous, savedAt)
		if !ok {
			continue
		}
		plan.clean[g.relDir] = true
		for path, digest := range reused {
			plan.reused[path] = digest
		}
	}
	return plan
}

func verifyPreviousOutputs(paths []string, previous map[string]cachedOutput, savedAt time.Time) (map[string][sha256.Size]byte, bool) {
	reused := map[string][sha256.Size]byte{}
	for _, path := range paths {
		output, recorded := previous[path]
		info, err := os.Lstat(path)
		if !recorded {
			if err == nil {
				return nil, false
			}
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() != output.Size || info.ModTime().UnixNano() != output.MTime {
			return nil, false
		}
		decoded, err := hex.DecodeString(output.Digest)
		if err != nil || len(decoded) != sha256.Size {
			return nil, false
		}
		var digest [sha256.Size]byte
		copy(digest[:], decoded)
		if savedAt.Sub(info.ModTime()) <= buildCacheRacy {
			data, err := os.ReadFile(path) // #nosec G304 -- a generated output path under the project.
			if err != nil || sha256.Sum256(data) != digest {
				return nil, false
			}
		}
		reused[path] = digest
	}
	return reused, true
}

// expectedGroupOutputs lists, per directory, the absolute paths of the
// per-directory outputs: the files eachChangedGroup would skip.
func expectedGroupOutputs(cfg *Config, idx *sourceIndex) map[string][]string {
	out := make(map[string][]string, len(idx.groups))
	for _, group := range idx.groups {
		rel := filepath.FromSlash(group.relDir)
		if rel == "." {
			rel = ""
		}
		e := expectedOutputs{}
		e.addGoOutputs(cfg, rel, group.relDir)
		e.addTSOutputs(cfg, rel, group.relDir)
		e.addPythonOutputs(cfg, rel, group.relDir)
		if cfg.Generate.OpenAPI != nil {
			filtered := filteredGroup(group, cfg.Generate.OpenAPI.ServiceFilter)
			for _, service := range filtered.file.Services {
				base := openAPIBasePath(filtered, service)
				e.add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".yaml")
				e.add(cfg.resolve(cfg.Generate.OpenAPI.Out), base+".json")
			}
		}
		for root, files := range e {
			for file := range files {
				out[group.relDir] = append(out[group.relDir], filepath.Join(root, file))
			}
		}
		sort.Strings(out[group.relDir])
	}
	return out
}
