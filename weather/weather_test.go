package weather

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/wisborg/fitactivity"
	"github.com/wisborg/fitactivity/effort"
	"github.com/wisborg/fitactivity/units"
	fw "github.com/wisborg/fitactivity/weather"
)

// Every value here is invented.

// The line every program says the weather in, in its units.
func TestLine(t *testing.T) {
	m, _ := units.Of(units.Metric)
	s := fw.Summary{TemperatureMin: 14.2, TemperatureMax: 16.4, DewPoint: 9, WindSpeed: 4.2, WindFrom: 250, Gusts: 9, WindSteadiness: 0.95}
	if got, want := Line(s, m), "14–16°C, dew point 9°C, wind 15 km/h from WSW, gusts 32"; got != want {
		t.Errorf("metric: %q, want %q", got, want)
	}
	imp, _ := units.Of(units.Imperial)
	s = fw.Summary{TemperatureMin: 20, TemperatureMax: 20.2, DewPoint: 10, WindSpeed: 0.1}
	if got := Line(s, imp); !strings.HasPrefix(got, "68°F,") || !strings.HasSuffix(got, "calm") {
		t.Errorf("imperial: %q", got)
	}
}

// Over a long activity the line says how the weather changed from the
// start to the end, and calls a wind that went every way variable.
func TestLine_Changing(t *testing.T) {
	m, _ := units.Of(units.Metric)
	at := func(temp, speed, from float64) fw.Conditions {
		return fw.Conditions{Temperature: temp, WindSpeed: speed, WindFrom: from}
	}
	for _, tc := range []struct {
		name string
		s    fw.Summary
		want string
	}{
		{"a marathon into the afternoon", fw.Summary{TemperatureMin: 10.2, TemperatureMax: 21.6, DewPoint: 6, WindSpeed: 4, WindFrom: 250, WindSteadiness: 0.98,
			Start: at(10.2, 3.5, 245), End: at(21.6, 4.5, 255)}, "10°C rising to 22°C, dew point 6°C, wind 14 km/h from WSW"},
		{"a front over a long ride", fw.Summary{TemperatureMin: 13.8, TemperatureMax: 24.1, DewPoint: 12, WindSpeed: 5, WindFrom: 200, WindSteadiness: 0.7,
			Start: at(22, 2.8, 250), End: at(14, 7.2, 135)}, "22°C falling to 14°C (14–24°C), dew point 12°C, wind 10 rising to 26 km/h from WSW, turning SE"},
		{"a wind that went round and back", fw.Summary{TemperatureMin: 15, TemperatureMax: 16, DewPoint: 8, WindSpeed: 2, WindFrom: 10, WindSteadiness: 0.3,
			Start: at(15, 2, 90), End: at(16, 2, 100)}, "15–16°C, dew point 8°C, wind 7 km/h, variable"},
	} {
		if got := Line(tc.s, m); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// With a weather code the line opens with the sky, and the words come
// apart for a program laying them out itself.
func TestWordsOf_Sky(t *testing.T) {
	m, _ := units.Of(units.Metric)
	s := fw.Summary{TemperatureMin: 12, TemperatureMax: 13, DewPoint: 10, WindSpeed: 3, WindFrom: 180, WindSteadiness: 1,
		HasSky: true, WeatherCode: 61}
	w := WordsOf(s, m)
	if w.Sky != "light rain" || w.Temperature != "12–13°C" || w.Wind != "wind 11 km/h from S" {
		t.Errorf("words %+v", w)
	}
	if got, want := Line(s, m), "light rain, 12–13°C, dew point 10°C, wind 11 km/h from S"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

// Each cost is said with what it rests on; one the athlete was
// acclimatised to is left out and said to be; an altitude below where
// oxygen uptake falls is not mentioned.
func TestWriteCosts(t *testing.T) {
	c := Costs{exposure: effort.Suburban, wind: 72 * time.Second, hasWind: true,
		windWorst: 0.0062, windBest: -0.0049, hasWindPace: true, paceOver: 1000, paceUnit: "km",
		heat: 3*time.Minute + 20*time.Second, wbgtLo: 21.2, wbgtHi: 23.6, hasHeat: true,
		altitude: 0, highest: 120, hasAltitude: true}
	var b strings.Builder
	WriteCosts(&b, c, true)
	for _, want := range []string{"the wind cost about 1 min 12 s over the run: up to +6 s/km into it and -5 s/km with it behind;",
		"suburban exposure (0.38", "the heat cost about 3 min 20 s, at WBGT 21–24 °C in the shade"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lacks %q:\n%s", want, b.String())
		}
	}
	if strings.Contains(b.String(), "altitude") {
		t.Errorf("an altitude of 120 m mentioned:\n%s", b.String())
	}

	c.acclim, c.highest, c.altitude = Acclimatisation{Heat: true}, 1800, 4*time.Minute
	b.Reset()
	WriteCosts(&b, c, true)
	for _, want := range []string{"the heat (WBGT 21–24 °C in the shade) is left out: you were acclimatised",
		"the altitude cost about 4 min, up to 1800 m, where 9.4% of oxygen uptake"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lacks %q:\n%s", want, b.String())
		}
	}
}

// JSON gives the weather, and the costs in seconds, leaving out the one
// the athlete was acclimatised to and saying so.
func TestJSONOf(t *testing.T) {
	t0 := time.Date(2026, 4, 2, 6, 0, 0, 0, time.UTC)
	tr := &fitactivity.Track{}
	for i := 0; i < 120; i++ {
		tr.Samples = append(tr.Samples, fitactivity.Sample{Time: t0.Add(time.Duration(i) * time.Second), HasGPS: true, Lat: 10, Lon: 20})
	}
	hours := []fw.Conditions{{Time: t0, Temperature: 12, WindSpeed: 3, WindFrom: 90}, {Time: t0.Add(time.Hour), Temperature: 14, WindSpeed: 3, WindFrom: 90}}
	p := Plan{Report: &fw.Report{Places: []fw.Place{{Lat: 10, Lon: 20, Hours: hours}}}}
	c := Costs{exposure: effort.Open, wind: 30 * time.Second, hasWind: true, heat: time.Minute, wbgtLo: 18, wbgtHi: 19, hasHeat: true,
		acclim: Acclimatisation{Heat: true}}
	j := JSONOf(p, tr, c, true)
	if j == nil || j.TemperatureMinC != 12 || j.WindFromDeg != 90 || *j.WindCostS != 30 || j.WindExposure != "open" ||
		j.HeatCostS != nil || *j.WBGTMaxC != 19 || j.TemperatureStartC != 12 || j.WindFromEndDeg != 90 || len(j.Acclimatised) != 1 || j.Acclimatised[0] != "heat" {
		t.Errorf("json %+v", j)
	}
	if j.WeatherCode != nil || j.Sky != "" {
		t.Errorf("a sky without a code: %+v", j)
	}
	for i := range hours {
		hours[i].HasSky, hours[i].WeatherCode, hours[i].Precipitation, hours[i].CloudCover = true, 80, 1.2, 70
	}
	if j := JSONOf(p, tr, c, true); j.WeatherCode == nil || *j.WeatherCode != 80 || j.Sky != "light showers" || *j.CloudCoverPct != 70 {
		t.Errorf("sky json %+v", j)
	}
	if JSONOf(Plan{}, tr, c, true) != nil {
		t.Error("weather without a report")
	}
}

// Where the wind was never behind, or never against, or nothing either
// way, the line says so rather than a "+0".
func TestWindAlong(t *testing.T) {
	c := Costs{paceOver: 1609.344, paceUnit: "mi"}
	for _, tc := range []struct {
		worst, best float64
		want        string
	}{
		{0.004, 0.001, "up to +6 s/mi into it, never behind"},
		{-0.0002, -0.003, "up to -5 s/mi with it behind, never into it"},
		{0.0002, -0.0002, "under a second a mile either way"},
	} {
		c.windWorst, c.windBest = tc.worst, tc.best
		if got := windAlong(c); got != tc.want {
			t.Errorf("%v, %v: %q, want %q", tc.worst, tc.best, got, tc.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	for d, want := range map[time.Duration]string{5 * time.Second: "5 s", 72 * time.Second: "1 min 12 s", 14*time.Minute + 20*time.Second: "14 min"} {
		if got := formatCost(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

// Without --weather nothing is fetched, and the flags only read with it
// say so; a flight asks for none; a bad value is refused.
func TestResolve(t *testing.T) {
	var o Options
	cmd := &cobra.Command{Use: "prog"}
	AddFlags(cmd.Flags(), &o, "the weather")
	var errs bytes.Buffer
	cmd.SetErr(&errs)
	if err := cmd.Flags().Parse([]string{"--wind-exposure", "open"}); err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(cmd, o, &fitactivity.Track{}, false)
	if err != nil || p.Asked || p.Exposure != effort.Open || !strings.Contains(errs.String(), "--wind-exposure is only read with --weather") {
		t.Errorf("plan %+v, %v, %q", p, err, errs.String())
	}
	o.Weather = true
	if p, err := Resolve(cmd, o, &fitactivity.Track{}, true); err != nil || p.Report != nil || !strings.Contains(p.Reason, "flight") {
		t.Errorf("flight: %+v, %v", p, err)
	}
	o.Acclimatised = []string{"humidity"}
	if _, err := Resolve(cmd, o, &fitactivity.Track{}, false); err == nil {
		t.Error("--acclimatised humidity accepted")
	}
}
