package onek

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1homsi/onekit/internal/onklang"
)

const parseCacheSettle = 2 * time.Second

type parsedSource struct {
	size     int64
	mtime    int64
	cachedAt int64
	text     string
	lines    []string
	ast      *onklang.File
	parseErr error
}

func (p *parsedSource) settled() bool {
	return p.cachedAt-p.mtime > int64(parseCacheSettle)
}

var sourceParses = struct {
	sync.Mutex
	entries map[string]*parsedSource
}{entries: map[string]*parsedSource{}}

func lookupParsedSource(path string, info os.FileInfo) *parsedSource {
	sourceParses.Lock()
	defer sourceParses.Unlock()
	entry := sourceParses.entries[path]
	if entry == nil || !entry.settled() || entry.size != info.Size() || entry.mtime != info.ModTime().UnixNano() {
		return nil
	}
	return entry
}

func storeParsedSource(path string, entry *parsedSource) {
	sourceParses.Lock()
	sourceParses.entries[path] = entry
	sourceParses.Unlock()
}

func pruneParsedSources(keep []string) {
	live := make(map[string]struct{}, len(keep))
	for _, path := range keep {
		live[path] = struct{}{}
	}
	sourceParses.Lock()
	for path := range sourceParses.entries {
		if _, ok := live[path]; !ok {
			delete(sourceParses.entries, path)
		}
	}
	sourceParses.Unlock()
}

func loadParsedSource(path string) (*parsedSource, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlink input %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input %s is not a regular file", path)
	}
	if info.Size() > maxInputFileBytes {
		return nil, fmt.Errorf("input %s exceeds the %d-byte limit", path, maxInputFileBytes)
	}
	if entry := lookupParsedSource(path, info); entry != nil {
		return entry, nil
	}
	cachedAt := time.Now().UnixNano()
	data, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	entry := &parsedSource{size: int64(len(data)), mtime: info.ModTime().UnixNano(), cachedAt: cachedAt, text: text, lines: strings.Split(text, "\n")}
	entry.ast, entry.parseErr = onklang.Parse(text)
	storeParsedSource(path, entry)
	return entry, nil
}
