// Package weather gives a program's commands --weather and what goes with
// it -- --wind-exposure, --acclimatised -- and says what the weather was and
// what it cost the same way in fitdash and course: the line of conditions,
// where they came from, the credit their licence asks for, and on a run the
// wind's, the heat's and the altitude's cost, each with what it rests on.
//
// The work is fitactivity's weather and effort packages'; this is only
// what the commands share. Fetching tells Open-Meteo where and when the
// activity was, so a command does it only with --weather.
package weather

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wisborg/fitactivity"
	"github.com/wisborg/fitactivity/effort"
	"github.com/wisborg/fitactivity/units"
	fw "github.com/wisborg/fitactivity/weather"
)

// Options are the flags as given: --weather, --wind-exposure and
// --acclimatised.
type Options struct {
	Weather      bool
	Exposure     string
	Acclimatised []string
}

// --wind-exposure's values: how much of the wind at 10 m reaches a runner
// a metre up, by the ground they ran over. See effort.Exposure.
const (
	ExposureOpen     = "open"
	ExposureSuburban = "suburban"
	ExposureUrban    = "urban"
)

var exposures = map[string]effort.Exposure{
	ExposureOpen: effort.Open, ExposureSuburban: effort.Suburban, ExposureUrban: effort.Urban,
}

// AddFlags gives f --weather, with help saying what the command does with
// it, and --wind-exposure and --acclimatised, into o.
func AddFlags(f *pflag.FlagSet, o *Options, help string) {
	f.BoolVar(&o.Weather, "weather", false, help)
	f.StringVar(&o.Exposure, "wind-exposure", ExposureSuburban, "with --weather, how much of the wind forecast at 10 m "+
		"reached you a metre up, by the ground you ran over: \"open\" (fields, a beach: 0.60 of it), \"suburban\" "+
		"(default: houses, gardens and trees, 0.38) or \"urban\" (between taller buildings, 0.25) -- the logarithmic "+
		"profile wind follows over ground that rough. It scales the wind's cost, not its direction")
	f.StringSliceVar(&o.Acclimatised, "acclimatised", nil, "with --weather, what you were acclimatised to -- "+
		"\"heat\", \"altitude\", or both, comma-separated -- whose cost is then left out: the figures for both are "+
		"for a runner not acclimatised, and nothing published says by how much acclimatisation reduces them")
}

// ParseExposure validates --wind-exposure.
func ParseExposure(name string) (effort.Exposure, error) {
	if x, ok := exposures[name]; ok {
		return x, nil
	}
	return 0, fmt.Errorf("--wind-exposure %q is invalid; use %s, %s or %s", name, ExposureOpen, ExposureSuburban, ExposureUrban)
}

// ExposureName is --wind-exposure's name for x.
func ExposureName(x effort.Exposure) string {
	for name, v := range exposures {
		if v == x {
			return name
		}
	}
	return fmt.Sprintf("%.2f", float64(x))
}

// Acclimatisation is what --acclimatised says the athlete is adapted to.
// Nothing published says how much acclimatisation takes off either loss,
// so an adjustment the athlete is adapted to is left out, and said to be,
// rather than guessed at.
type Acclimatisation struct{ Heat, Altitude bool }

// ParseAcclimatised validates --acclimatised.
func ParseAcclimatised(names []string) (Acclimatisation, error) {
	var a Acclimatisation
	for _, n := range names {
		switch strings.TrimSpace(n) {
		case "heat":
			a.Heat = true
		case "altitude":
			a.Altitude = true
		case "":
		default:
			return a, fmt.Errorf("--acclimatised %q is invalid; use heat, altitude or both", n)
		}
	}
	return a, nil
}

// Plan is what --weather did: the weather fetched, or why there is none,
// and the exposure and acclimatisation its costs are worked out at.
type Plan struct {
	// Asked is whether --weather was given.
	Asked bool
	// Report is the weather, or nil with Reason saying why there is none.
	Report *fw.Report
	Reason string

	Exposure effort.Exposure
	Acclim   Acclimatisation
}

// Resolve validates o and fetches the weather track was done in, when
// --weather asks for it. A weather that cannot be fetched is not an error
// -- a command goes on without it -- and Reason says why. A flight gets
// none: the ground's weather is no account of the air's at altitude.
func Resolve(cmd *cobra.Command, o Options, track *fitactivity.Track, flight bool) (Plan, error) {
	x, err := ParseExposure(o.Exposure)
	if err != nil {
		return Plan{}, err
	}
	a, err := ParseAcclimatised(o.Acclimatised)
	if err != nil {
		return Plan{}, err
	}
	for _, f := range []string{"wind-exposure", "acclimatised"} {
		if !o.Weather && cmd.Flags().Changed(f) {
			fmt.Fprintf(cmd.ErrOrStderr(), "--%s is only read with --weather\n", f)
		}
	}
	p := Plan{Asked: o.Weather, Exposure: x, Acclim: a}
	if !o.Weather {
		return p, nil
	}
	if flight {
		p.Reason = "a flight is above the weather at the ground, which is all there is to fetch"
		return p, nil
	}
	dir, err := fw.DefaultCacheDir()
	if err != nil {
		dir = ""
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	r, err := fw.New(dir).Fetch(ctx, track)
	switch {
	case errors.Is(err, fw.ErrNoPosition):
		p.Reason = "the activity records no position to ask the weather for"
	case err != nil:
		p.Reason = err.Error()
	default:
		p.Report = r
	}
	return p, nil
}

// Line is the weather over an activity in a line: "14–16°C, dew point
// 9°C, wind 15 km/h from WSW, gusts 25". Where it changed over the
// activity -- a marathon into the afternoon's heat, a long ride a front
// passed over -- it says how, from the start to the end: "12°C rising to
// 24°C", "wind 10 rising to 25 km/h from WSW, turning SE"; and a wind that
// turned about without settling is "variable" rather than given an
// average direction it never blew from. Every place a program says it, it
// says it in these words.
func Line(s fw.Summary, u units.Set) string {
	t := func(v float64) string { return fmt.Sprintf("%.0f", u.Temperature.FromSI(v)) }
	deg := u.Temperature.Name
	temp := t(s.TemperatureMin) + deg
	if lo, hi := t(s.TemperatureMin), t(s.TemperatureMax); lo != hi {
		temp = lo + "–" + hi + deg
	}
	if rise := s.End.Temperature - s.Start.Temperature; math.Abs(rise) >= tempChange {
		way := "rising"
		if rise < 0 {
			way = "falling"
		}
		from, to := t(s.Start.Temperature), t(s.End.Temperature)
		temp = from + deg + " " + way + " to " + to + deg
		if lo, hi := t(s.TemperatureMin), t(s.TemperatureMax); (lo != from && lo != to) || (hi != from && hi != to) {
			temp += " (" + lo + "–" + hi + deg + ")"
		}
	}
	parts := []string{temp, "dew point " + t(s.DewPoint) + deg}
	if s.WindSpeed < 0.3 {
		return strings.Join(append(parts, "calm"), ", ")
	}
	v := func(ms float64) string { return fmt.Sprintf("%.0f", u.Speed.FromSI(ms)) }
	wind := "wind " + v(s.WindSpeed) + " " + u.Speed.Name
	if change := s.End.WindSpeed - s.Start.WindSpeed; math.Abs(change) >= windChange {
		way := "rising"
		if change < 0 {
			way = "easing"
		}
		wind = "wind " + v(s.Start.WindSpeed) + " " + way + " to " + v(s.End.WindSpeed) + " " + u.Speed.Name
	}
	switch {
	case s.Start.WindSpeed >= 1 && s.End.WindSpeed >= 1 && fw.Apart(s.Start.WindFrom, s.End.WindFrom) >= windTurn:
		wind += " from " + fw.Compass(s.Start.WindFrom) + ", turning " + fw.Compass(s.End.WindFrom)
	case s.WindSteadiness < windSteady:
		wind += ", variable"
	default:
		wind += " from " + fw.Compass(s.WindFrom)
	}
	if gust := u.Speed.FromSI(s.Gusts); gust >= u.Speed.FromSI(s.WindSpeed)+5 {
		wind += fmt.Sprintf(", gusts %.0f", gust)
	}
	return strings.Join(append(parts, wind), ", ")
}

// What Line calls a change from the start to the end of an activity: 3 °C
// warmer or cooler, which an hour's run seldom meets and a marathon into
// the afternoon often does; a wind 3 m/s (11 km/h) stronger or lighter;
// and one that turned through 45°, two points of the compass either way.
// Short of a turn, a wind whose average as a vector is less than half its
// average speed went too many ways to be said to come from one.
const (
	tempChange = 3.0
	windChange = 3.0
	windTurn   = 45.0
	windSteady = 0.5
)

// WriteNote says what the weather was, whether this run told Open-Meteo
// where and when the activity was -- only when it did, so the one line
// about what was sent is never noise -- and the credit the data's licence
// asks for wherever they are shown, which whoever publishes what the
// program made, publication, owes too.
func WriteNote(out io.Writer, p Plan, track *fitactivity.Track, u units.Set, publication string) {
	if !p.Asked {
		return
	}
	if p.Report == nil {
		fmt.Fprintf(out, "weather: none -- %s\n", p.Reason)
		return
	}
	origin := "the area and hours of this activity were sent to open-meteo.com"
	if p.Report.Cached {
		origin = "served from your cache; nothing was sent this run"
	}
	line := "weather: " + origin
	if s, ok := fw.Summarise(p.Report, track); ok {
		line = "weather: " + Line(s, u) + " -- " + origin
	}
	fmt.Fprintln(out, line)
	fmt.Fprintf(out, "  a model's weather, not a measurement: the wind is at 10 m in the open. %s (CC BY 4.0, %s), "+
		"from ECMWF's IFS and Copernicus's ERA5 (contains modified Copernicus Climate Change Service information); "+
		"credit it wherever you publish %s\n", fw.Credit, fw.CreditURL, publication)
}

// OnFoot says the sport is one the effort model is of: running, walking,
// hiking.
func OnFoot(sport string) bool {
	s := strings.ToLower(sport)
	return strings.Contains(s, "run") || s == "walking" || s == "hiking"
}

// Grade is the grade at each sample of track on elev, measured over 25 m
// either side as the effort model's is; nothing where elev has none.
func Grade(track *fitactivity.Track, elev *fitactivity.ElevationModel) func(i int) float64 {
	return func(i int) float64 {
		s := track.Samples[i]
		if elev == nil || elev.Empty() || !s.HasDistance {
			return 0
		}
		return elev.GradeAtDistance(s.Distance, 25)
	}
}

// Costs are what the conditions cost over a run, by the effort model: the
// wind at the exposure assumed, and the heat and the altitude unless the
// athlete was acclimatised to them.
type Costs struct {
	exposure effort.Exposure
	acclim   Acclimatisation

	wind    time.Duration
	hasWind bool
	// windWorst and windBest are the wind's cost on the pace at its worst
	// and best, in seconds a metre (fw.WindPace); paceOver and paceUnit
	// the distance and name the pace is said over: 1000 m and "km", or a
	// mile.
	windWorst, windBest float64
	hasWindPace         bool
	paceOver            float64
	paceUnit            string

	heat           time.Duration
	wbgtLo, wbgtHi float64
	hasHeat        bool

	altitude    time.Duration
	highest     float64
	hasAltitude bool

	footpodFrom  float64
	hasFootpod   bool
	forecastFrom float64
}

// CostsOf is what the conditions p fetched cost over track, on grade, for
// an activity on foot -- what the effort model is of -- and nothing
// otherwise.
func CostsOf(p Plan, track *fitactivity.Track, u units.Set, grade func(i int) float64) (Costs, bool) {
	if p.Report == nil || !OnFoot(track.Sport) {
		return Costs{}, false
	}
	c := Costs{exposure: p.Exposure, acclim: p.Acclim, paceOver: 1000, paceUnit: "km"}
	if strings.HasSuffix(u.Pace.Name, "/mi") {
		c.paceOver, c.paceUnit = 1609.344, "mi"
	}
	r, x := p.Report, p.Exposure
	c.wind, c.hasWind = fw.WindCost(r, track, x, grade)
	c.windWorst, c.windBest, c.hasWindPace = fw.WindPace(r, track, x, grade)
	c.heat, c.wbgtLo, c.wbgtHi, c.hasHeat = fw.HeatCost(r, track)
	c.altitude, c.highest, c.hasAltitude = fw.AltitudeCost(r, track, x, grade)
	if from, _, ok := fw.FootpodWind(track); ok {
		c.footpodFrom, c.hasFootpod = from, true
		if s, ok := fw.Summarise(r, track); ok {
			c.forecastFrom = s.WindFrom
		}
	}
	return c, true
}

// WriteCosts says what the conditions cost over the run, each with what it
// rests on: the wind at the exposure assumed, which is its largest
// uncertainty, with what it did on the pace into it and behind, since on a
// loop the two nearly cancel in the total; the heat by the WBGT met and the
// race data it is from; the altitude by the highest point and the oxygen
// uptake lost there -- and where a Stryd pod recorded its air power, which
// way that says the wind blew beside the forecast's: a check on the
// model's direction, its watts being the pod's own reckoning from the shoe.
func WriteCosts(out io.Writer, c Costs, ok bool) {
	if !ok {
		return
	}
	if c.hasWind {
		cost, verb := c.wind, "cost"
		if cost < 0 {
			verb, cost = "gave", -cost
		}
		along := ""
		if c.hasWindPace {
			along = ": " + windAlong(c)
		}
		fmt.Fprintf(out, "  the wind %s about %s over the run%s; by Pugh's measure of the air's cost, at %s exposure (%.2f of the 10 m wind)\n",
			verb, formatCost(cost), along, ExposureName(c.exposure), float64(c.exposure))
	}
	if c.hasHeat {
		wbgt := fmt.Sprintf("WBGT %.0f °C", c.wbgtLo)
		if fmt.Sprintf("%.0f", c.wbgtLo) != fmt.Sprintf("%.0f", c.wbgtHi) {
			wbgt = fmt.Sprintf("WBGT %.0f–%.0f °C", c.wbgtLo, c.wbgtHi)
		}
		// In the shade: the sun's own heat is not in it (effort.WBGT).
		wbgt += " in the shade"
		switch {
		case c.acclim.Heat:
			fmt.Fprintf(out, "  the heat (%s) is left out: you were acclimatised to it\n", wbgt)
		case c.heat < time.Second:
			fmt.Fprintf(out, "  the heat cost nothing: %s, within the %.1f–%.0f °C races go best in\n", wbgt, effort.CoolestBest, effort.WarmestBest)
		default:
			what := "heat"
			if c.wbgtHi < effort.CoolestBest {
				what = "cold"
			}
			fmt.Fprintf(out, "  the %s cost about %s, at %s, by Mantzios and others' race data, for a runner not acclimatised to it\n",
				what, formatCost(c.heat), wbgt)
		}
	}
	if c.hasAltitude {
		switch {
		case c.highest <= effort.AltitudeThreshold:
			// Nothing to say: most runs are here, and a line saying the
			// altitude cost nothing on every one of them is noise.
		case c.acclim.Altitude:
			fmt.Fprintf(out, "  the altitude (up to %.0f m) is left out: you were acclimatised to it\n", c.highest)
		default:
			fmt.Fprintf(out, "  the altitude cost about %s, up to %.0f m, where %.1f%% of oxygen uptake is lost on arrival (Wehrlin and Hallén)\n",
				formatCost(c.altitude), c.highest, 100*effort.AltitudeLoss(c.highest))
		}
	}
	if c.hasFootpod {
		fmt.Fprintf(out, "  Stryd's air power was highest running %s, so a wind from there; the forecast's was from %s, %.0f° apart\n",
			fw.Compass(c.footpodFrom), fw.Compass(c.forecastFrom), fw.Apart(c.footpodFrom, c.forecastFrom))
	}
}

// windAlong is what the wind did on the pace along the run: "up to +6 s/km
// into it and -5 s/km with it behind", or, where it was never behind, how
// much it cost at least.
func windAlong(c Costs) string {
	s := func(v float64) float64 { return math.Round(v * c.paceOver) }
	worst, best := s(c.windWorst), s(c.windBest)
	switch {
	case worst <= 0 && best >= 0:
		word := map[string]string{"km": "kilometre", "mi": "mile"}[c.paceUnit]
		return "under a second a " + word + " either way"
	case best >= 0:
		return fmt.Sprintf("up to %+.0f s/%s into it, never behind", worst, c.paceUnit)
	case worst <= 0:
		return fmt.Sprintf("up to %+.0f s/%s with it behind, never into it", best, c.paceUnit)
	}
	return fmt.Sprintf("up to %+.0f s/%s into it and %+.0f s/%s with it behind", worst, c.paceUnit, best, c.paceUnit)
}

// formatCost is a time a condition cost: "25 s", "1 min 12 s", "14 min".
func formatCost(d time.Duration) string {
	switch s := int(d.Round(time.Second).Seconds()); {
	case d >= 10*time.Minute:
		return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
	case s >= 60 && s%60 == 0:
		return fmt.Sprintf("%d min", s/60)
	case s >= 60:
		return fmt.Sprintf("%d min %d s", s/60, s%60)
	default:
		return fmt.Sprintf("%d s", s)
	}
}

// JSON is the weather over an activity for JSON and YAML, in SI units and
// the same names in every program: the wind at 10 m, from the direction it
// blew from; and on a run what the conditions cost, in seconds over it,
// left out where there was nothing to work one out from or the athlete
// was acclimatised.
type JSON struct {
	TemperatureMinC float64 `json:"temperature_min_c" yaml:"temperature_min_c"`
	TemperatureMaxC float64 `json:"temperature_max_c" yaml:"temperature_max_c"`
	DewPointC       float64 `json:"dew_point_c" yaml:"dew_point_c"`
	HumidityPct     float64 `json:"humidity_pct" yaml:"humidity_pct"`
	WindSpeedMS     float64 `json:"wind_speed_ms" yaml:"wind_speed_ms"`
	WindFromDeg     float64 `json:"wind_from_deg" yaml:"wind_from_deg"`
	GustsMS         float64 `json:"gusts_ms" yaml:"gusts_ms"`
	RadiationWM2    float64 `json:"radiation_w_m2" yaml:"radiation_w_m2"`
	// The weather at the start and the end, which over a long activity the
	// averages above hide, and how steady the wind's direction was: 1 for
	// one that never turned, near 0 for one that blew every way.
	TemperatureStartC float64 `json:"temperature_start_c" yaml:"temperature_start_c"`
	TemperatureEndC   float64 `json:"temperature_end_c" yaml:"temperature_end_c"`
	WindSpeedStartMS  float64 `json:"wind_speed_start_ms" yaml:"wind_speed_start_ms"`
	WindSpeedEndMS    float64 `json:"wind_speed_end_ms" yaml:"wind_speed_end_ms"`
	WindFromStartDeg  float64 `json:"wind_from_start_deg" yaml:"wind_from_start_deg"`
	WindFromEndDeg    float64 `json:"wind_from_end_deg" yaml:"wind_from_end_deg"`
	WindSteadiness    float64 `json:"wind_steadiness" yaml:"wind_steadiness"`
	Source            string  `json:"source" yaml:"source"`

	WindCostS    *float64 `json:"wind_cost_s,omitempty" yaml:"wind_cost_s,omitempty"`
	WindExposure string   `json:"wind_exposure,omitempty" yaml:"wind_exposure,omitempty"`
	// The wind's cost on the pace at its worst and best, in seconds a
	// kilometre: positive into it, negative with it behind.
	WindWorstSPerKm *float64 `json:"wind_worst_s_per_km,omitempty" yaml:"wind_worst_s_per_km,omitempty"`
	WindBestSPerKm  *float64 `json:"wind_best_s_per_km,omitempty" yaml:"wind_best_s_per_km,omitempty"`
	WBGTMinC        *float64 `json:"wbgt_min_c,omitempty" yaml:"wbgt_min_c,omitempty"`
	WBGTMaxC        *float64 `json:"wbgt_max_c,omitempty" yaml:"wbgt_max_c,omitempty"`
	HeatCostS       *float64 `json:"heat_cost_s,omitempty" yaml:"heat_cost_s,omitempty"`
	HighestM        *float64 `json:"highest_m,omitempty" yaml:"highest_m,omitempty"`
	AltitudeCostS   *float64 `json:"altitude_cost_s,omitempty" yaml:"altitude_cost_s,omitempty"`
	Acclimatised    []string `json:"acclimatised,omitempty" yaml:"acclimatised,omitempty"`
}

// JSONOf is the weather p fetched over track for JSON and YAML, with the
// costs c where hasCosts; nil without weather.
func JSONOf(p Plan, track *fitactivity.Track, c Costs, hasCosts bool) *JSON {
	if p.Report == nil {
		return nil
	}
	s, ok := fw.Summarise(p.Report, track)
	if !ok {
		return nil
	}
	j := &JSON{TemperatureMinC: s.TemperatureMin, TemperatureMaxC: s.TemperatureMax,
		DewPointC: s.DewPoint, HumidityPct: s.Humidity, WindSpeedMS: s.WindSpeed, WindFromDeg: s.WindFrom,
		GustsMS: s.Gusts, RadiationWM2: s.Radiation,
		TemperatureStartC: s.Start.Temperature, TemperatureEndC: s.End.Temperature,
		WindSpeedStartMS: s.Start.WindSpeed, WindSpeedEndMS: s.End.WindSpeed,
		WindFromStartDeg: s.Start.WindFrom, WindFromEndDeg: s.End.WindFrom, WindSteadiness: s.WindSteadiness,
		Source: fw.Credit + " (CC BY 4.0, " + fw.CreditURL + ")"}
	if !hasCosts {
		return j
	}
	secs := func(d time.Duration) *float64 { v := d.Seconds(); return &v }
	val := func(v float64) *float64 { return &v }
	if c.hasWind {
		j.WindCostS, j.WindExposure = secs(c.wind), ExposureName(c.exposure)
	}
	if c.hasWindPace {
		j.WindWorstSPerKm, j.WindBestSPerKm = val(c.windWorst*1000), val(c.windBest*1000)
	}
	if c.hasHeat {
		j.WBGTMinC, j.WBGTMaxC = val(c.wbgtLo), val(c.wbgtHi)
		if !c.acclim.Heat {
			j.HeatCostS = secs(c.heat)
		}
	}
	if c.hasAltitude {
		j.HighestM = val(c.highest)
		if !c.acclim.Altitude {
			j.AltitudeCostS = secs(c.altitude)
		}
	}
	for name, on := range map[string]bool{"altitude": c.acclim.Altitude, "heat": c.acclim.Heat} {
		if on {
			j.Acclimatised = append(j.Acclimatised, name)
		}
	}
	sort.Strings(j.Acclimatised)
	return j
}
