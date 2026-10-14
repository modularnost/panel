package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRecordKeepsTenMinutesPerService(t *testing.T) {
	history = map[string][]sample{}
	now := time.Unix(1_000_000, 0)
	for i := range keep + 30 {
		record([]stat{
			{Service: "web", Slot: 1, CPU: 10, Mem: 100, MemLimit: 1000},
			{Service: "web", Slot: 2, CPU: 5, Mem: 50, MemLimit: 1000},
			{Service: "db", Node: "w2", Err: "node unreachable"}, // no numbers: no sample
		}, now.Add(time.Duration(i)*interval))
	}
	if len(history["web"]) != keep || len(history) != 1 {
		t.Fatalf("history: %d samples of web, %d services", len(history["web"]), len(history))
	}
	// A service is one line: its replicas are summed.
	if s := history["web"][0]; s.CPU != 15 || s.Mem != 150 || s.MemLimit != 2000 || s.Replicas != 2 {
		t.Errorf("sample %+v", s)
	}
}

func TestOverviewShowsAnUnreachableNodeInsteadOfDroppingIt(t *testing.T) {
	d := overview([]stat{
		{Node: "w1", Service: "web", Slot: 1, CPU: 3, Mem: 10},
		{Node: "w2", Service: "web", Slot: 2, Err: "no agent"},
	})
	table := d.Blocks[len(d.Blocks)-1]
	rows := table["rows"].([]block)
	if table["type"] != "table" || len(rows) != 2 {
		t.Fatalf("%+v", table)
	}
	if bad := rows[1]["cpu"].(block); bad["text"] != "no agent" || bad["tone"] != "bad" {
		t.Errorf("the failing node: %+v", rows[1])
	}
	if link := rows[0]["service"].(block)["href"].(block); link["path"] != "/service?service=web" {
		t.Errorf("link: %+v", link)
	}
	if d.Blocks[1]["value"] != 1 { // only the replica that answered is running
		t.Errorf("running replicas: %+v", d.Blocks[1])
	}
}

func TestServiceDocumentCutsTheRangeAndHasChartsThatShareAnAxis(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var h []sample
	for i := range 30 { // 30 samples, 5 s apart: the last 150 s
		h = append(h, sample{T: now.Add(-time.Duration(29-i) * interval).Unix(), CPU: float64(i), Mem: 100, MemLimit: 1000})
	}
	d := serviceDoc("web", "1m", h, now)

	var charts []block
	for _, b := range d.Blocks {
		if b["type"] == "chart" {
			charts = append(charts, b)
		}
	}
	if len(charts) != 2 || charts[0]["sync"] != charts[1]["sync"] {
		t.Fatalf("charts: %+v", charts)
	}
	points := charts[0]["series"].([]block)[0]["points"].([][2]float64)
	if len(points) != 13 { // from now-60s through now-0: 12 intervals and the sample at the edge
		t.Errorf("a minute of samples is %d points", len(points))
	}
	if d.Blocks[0]["type"] != "choice" || d.Blocks[0]["value"] != "1m" {
		t.Errorf("range choice: %+v", d.Blocks[0])
	}
	// What the panel gets is JSON it can read.
	raw, err := json.Marshal(d)
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
}

func TestServiceDocumentEdges(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	if d := serviceDoc("", "", nil, now); d.Blocks[0]["type"] != "banner" {
		t.Errorf("no service asks for one: %+v", d.Blocks)
	}
	// With no history the charts say so, and an unknown range is five minutes.
	d := serviceDoc("web", "forever", nil, now)
	if d.Blocks[0]["value"] != "5m" {
		t.Errorf("range: %+v", d.Blocks[0])
	}
	for _, b := range d.Blocks {
		if b["type"] == "meter" {
			t.Errorf("a meter with nothing to measure: %+v", b)
		}
	}
	// A service with no memory limit has no memory meter, not a division by zero.
	d = serviceDoc("web", "5m", []sample{{T: now.Unix(), CPU: 150, Mem: 5}}, now)
	meters := 0
	for _, b := range d.Blocks {
		if b["type"] == "meter" {
			meters++
			if b["max"].(float64) < 150 {
				t.Errorf("a CPU above one core still fits its meter: %+v", b)
			}
		}
	}
	if meters != 1 {
		t.Errorf("%d meters", meters)
	}
}
