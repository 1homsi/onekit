package onek

import (
	"sync"
	"sync/atomic"
)

type BuildSummary struct {
	Targets []string
	Files   int
	Cached  bool
}

func BuildWithSummary(dir string) (BuildSummary, error) {
	writtenFiles.Store(0)
	cached, err := build(dir)
	summary := BuildSummary{Files: int(writtenFiles.Load()), Cached: cached}
	if cfg, cfgErr := LoadConfig(dir); cfgErr == nil {
		summary.Targets = cfg.EnabledTargets()
	}
	return summary, err
}

var (
	writtenFiles   atomic.Int64
	captureWriteMu sync.Mutex
)

// Build parses and compiles every .onk file under dir, then generates every
// target configured in onekit.toml.
func Build(dir string) error {
	_, err := build(dir)
	return err
}

func build(dir string) (bool, error) {
	resetSymlinkCheckCache()
	cfg, err := LoadConfig(dir)
	if err != nil {
		return false, err
	}

	startFiles := writtenFiles.Load()
	fingerprint := ""
	if captured == nil {
		if sum, sumErr := buildFingerprint(cfg); sumErr == nil {
			fingerprint = sum
		}
	}
	if fingerprint != "" && tryCachedBuild(cfg, fingerprint) {
		return true, nil
	}
	recording := fingerprint != ""
	exeHash := make(chan string, 1)
	if recording {
		go func() {
			hash, _ := executableHash()
			exeHash <- hash
		}()
		startRecordingOutputs()
		defer func() {
			if recording {
				stopRecordingOutputs()
			}
		}()
	}

	pkg, err := compileTree(cfg.SchemaDir(), cfg.schemaExtensions(), cfg.CompileOptions())
	if err != nil {
		return false, err
	}
	applyRoutePrefix(pkg, cfg.RoutePrefix)
	idx, err := groupByDirectory(pkg, cfg.SchemaDir())
	if err != nil {
		return false, err
	}

	for _, named := range cfg.serviceFilters() {
		if err := named.filter.checkMatches(named.target, idx); err != nil {
			return false, err
		}
		if err := named.filter.checkPackageMatches(named.target, idx); err != nil {
			return false, err
		}
		if err := idx.checkSelfContained(named.target, idx.view(named.filter)); err != nil {
			return false, err
		}
	}

	steps := []struct {
		enabled bool
		run     func() error
	}{
		{cfg.Generate.GoServer != nil || cfg.Generate.GoClient != nil, func() error { return buildGo(cfg, idx) }},
		{cfg.Generate.TSClient != nil, func() error { return buildTSClient(cfg, idx.view(cfg.Generate.TSClient.ServiceFilter)) }},
		{cfg.Generate.TSServer != nil, func() error { return buildTSServer(cfg, idx.view(cfg.Generate.TSServer.ServiceFilter)) }},
		{cfg.Generate.PythonClient != nil, func() error { return buildPythonClient(cfg, idx.view(cfg.Generate.PythonClient.ServiceFilter)) }},
		{cfg.Generate.DartClient != nil, func() error { return buildDartClient(cfg, idx.view(cfg.Generate.DartClient.ServiceFilter)) }},
		{cfg.Generate.SwiftClient != nil, func() error { return buildSwiftClient(cfg, idx.view(cfg.Generate.SwiftClient.ServiceFilter)) }},
		{cfg.Generate.RustClient != nil || cfg.Generate.RustServer != nil, func() error { return buildRust(cfg, idx) }},
		{cfg.Generate.OpenAPI != nil, func() error { return buildOpenAPI(cfg, idx.view(cfg.Generate.OpenAPI.ServiceFilter)) }},
	}
	for _, step := range steps {
		if !step.enabled {
			continue
		}
		err = step.run()
		if err != nil {
			return false, err
		}
	}
	if captured != nil {
		return false, nil
	}
	if err := cleanupStaleGeneratedOutputs(cfg, idx); err != nil {
		return false, err
	}
	if err := writeGenerationManifest(cfg, idx); err != nil {
		return false, err
	}
	if recording {
		digests := stopRecordingOutputs()
		recording = false
		saveBuildCache(cfg, fingerprint, <-exeHash, digests, int(writtenFiles.Load()-startFiles))
	}
	return false, nil
}
