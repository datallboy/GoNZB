package pgindex

import "testing"

func TestNormalizePAR2CoverageTargetsCountsMainArchiveTargets(t *testing.T) {
	rows := []BinaryPAR2TargetRecord{
		{FileName: "95da44375475e6adf5dc90acff76194d.part01.rar"},
		{FileName: "95da44375475e6adf5dc90acff76194d.part02.rar"},
		{FileName: "95da44375475e6adf5dc90acff76194d.sfv"},
		{FileName: "95da44375475e6adf5dc90acff76194d.vol00+01.par2"},
	}

	targets := normalizePAR2CoverageTargets(rows)
	if len(targets) != 2 {
		t.Fatalf("expected two archive targets, got %+v", targets)
	}
	if targets[0].baseStem != "95da44375475e6adf5dc90acff76194d" ||
		targets[1].baseStem != "95da44375475e6adf5dc90acff76194d" {
		t.Fatalf("expected shared base stem, got %+v", targets)
	}
	if targets[0].fileIndex != 1 || targets[1].fileIndex != 2 {
		t.Fatalf("expected rar part indexes 1/2, got %+v", targets)
	}
	stem, ok := singlePAR2CoverageBaseStem(targets)
	if !ok || stem != "95da44375475e6adf5dc90acff76194d" {
		t.Fatalf("expected one target stem, got %q ok=%v", stem, ok)
	}
}

func TestPAR2CoverageFileIndexSupportsArchiveFamilies(t *testing.T) {
	cases := map[string]int{
		"sample.rar":         1,
		"sample.part003.rar": 3,
		"sample.r00":         2,
		"sample.7z.004":      4,
		"sample.zip.005":     5,
	}
	for fileName, want := range cases {
		if got := par2CoverageFileIndex(fileName); got != want {
			t.Fatalf("expected %s index %d, got %d", fileName, want, got)
		}
	}
}

func TestPAR2CoverageUpdateChunkSizeStaysUnderPostgresParameterLimit(t *testing.T) {
	const fixedArgs = 2
	const argsPerUpdatedBinary = 3

	totalArgs := fixedArgs + (par2CoverageUpdateChunkSize * argsPerUpdatedBinary)
	if totalArgs >= 65535 {
		t.Fatalf("par2 coverage update chunk size %d yields %d bind args; must stay below postgres 65535 limit", par2CoverageUpdateChunkSize, totalArgs)
	}
}
