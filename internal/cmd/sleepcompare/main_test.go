package main

import (
	"strings"
	"testing"
)

func TestMedian(t *testing.T) {
	for _, test := range []struct {
		values []int64
		want   int64
	}{
		{values: []int64{9}, want: 9},
		{values: []int64{9, 1, 5}, want: 5},
		{values: []int64{10, 2, 8, 4}, want: 6},
	} {
		if got := median(test.values); got != test.want {
			t.Fatalf("median(%v) = %d, want %d", test.values, got, test.want)
		}
	}
}

func TestParseJavaMeasurements(t *testing.T) {
	got, err := parseJavaMeasurements("diagnostic\nRESULT\tarithmetic-loop\t123\t456\t499500\n")
	if err != nil {
		t.Fatal(err)
	}
	want := measurement{compileNS: 123, executeNS: 456, result: "499500"}
	if got["arithmetic-loop"] != want {
		t.Fatalf("measurement = %#v, want %#v", got["arithmetic-loop"], want)
	}
}

func TestWriteReport(t *testing.T) {
	config := configuration{samples: 3, warmup: 2, executeIterations: 4, compileIterations: 5}
	items := []comparison{{
		workload: workload{name: "arithmetic-loop"},
		opfor:    measurement{compileNS: 100, executeNS: 200},
		java:     measurement{compileNS: 400, executeNS: 100},
	}}
	var output strings.Builder
	writeReport(&output, config, "java test", items)
	for _, fragment := range []string{"# OPFOR vs official Sleep 2.1", "| arithmetic-loop | 200 | 100 | 2.00x |", "| arithmetic-loop | 100 | 400 | 0.25x |"} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, output.String())
		}
	}
}
