package services

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdm/sites-tool/internal/domain"
)

// Parsing a Kinsta log means parsing nginx, not PHP. PHP errors arrive wrapped:
//
//	2026/08/02 05:22:54 [error] 97649#97649: *211016 FastCGI sent in stderr:
//	"PHP message: PHP Fatal error:  Uncaught Error: ... in /www/x/web/public/f.php:34
//	Stack trace: #0 {main}   thrown in /www/x/web/public/f.php on line 34"
//	while reading response header from upstream, client: 1.2.3.4, server: x.nl, ...
//
// Everything below is built around that shape, verified against a real log.

var (
	// reErrorHead splits the nginx error.log prefix from the message.
	reErrorHead = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[([a-z]+)\] \d+#\d+: (?:\*\d+ )?(.*)$`)

	// reFastCGI pulls the PHP payload out of the nginx wrapper. The capture is
	// greedy on purpose: the payload itself contains quotes and commas, so only
	// the last `" while reading ... from upstream` may end it.
	reFastCGI = regexp.MustCompile(`(?s)^FastCGI sent in stderr: "(.*)" while reading .*?from upstream(.*)$`)

	rePHPLevel = regexp.MustCompile(`^PHP message:\s*PHP (Fatal error|Parse error|Recoverable fatal error|Warning|Notice|Deprecated):\s*(.*)$`)
	rePHPAny   = regexp.MustCompile(`^PHP message:\s*(.*)$`)

	// Two shapes name the failing file. Fatals use `in FILE:LINE` and repeat it
	// as `thrown in FILE on line N`; warnings only use the latter shape.
	reThrownIn      = regexp.MustCompile(`thrown in (/[^\s"]+\.php) on line (\d+)`)
	reInFileColon   = regexp.MustCompile(` in (/[^\s"]+\.php):(\d+)`)
	reInFileOnLine  = regexp.MustCompile(` in (/[^\s"]+\.php) on line (\d+)`)
	reMetaClient    = regexp.MustCompile(`\bclient: ([0-9a-fA-F.:]+)`)
	reMetaRequest   = regexp.MustCompile(`\brequest: "([^"]*)"`)
	reMetaHost      = regexp.MustCompile(`\bhost: "([^"]*)"`)
	reQuotedContent = regexp.MustCompile(`"[^"]*"`)
	reCijfers       = regexp.MustCompile(`\d+`)
	reWitruimte     = regexp.MustCompile(`\s+`)

	// reAccess matches the Kinsta access.log line format:
	// HOST IP [02/Jan/2006:15:04:05 -0700] METHOD "PATH" PROTO STATUS ...
	reAccess = regexp.MustCompile(`^(\S+) (\S+) \[([^\]]+)\] (\S+) "([^"]*)" (\S+) (\d{3})`)

	// reCachePerf matches the kinsta-cache-perf.log line format:
	// [02/Jan/2006:15:04:05 -0700] CACHESTATUS KINSTAWP IP METHOD "PATH" PROTO STATUS ...
	reCachePerf = regexp.MustCompile(`^\[([^\]]+)\] (\S+) (\S+) (\S+) (\S+) "([^"]*)" (\S+) (\d{3})`)
)

const (
	// nginxTijdLayout is the error.log timestamp. It carries no zone; Kinsta
	// containers run UTC, which is what the access log's explicit +0000 shows.
	nginxTijdLayout  = "2006/01/02 15:04:05"
	accessTijdLayout = "02/Jan/2006:15:04:05 -0700"

	// maxRuweRegel caps what we keep per sample. nginx truncates long FastCGI
	// payloads itself, but a print_r dump can still be kilobytes.
	maxRuweRegel = 2000
	// maxTitel caps a group title so the UI stays readable.
	maxTitel = 300
	// maxVoorbeelden is how many raw lines we keep per group.
	maxVoorbeelden = 3
)

// parseLogFile turns raw log contents into entries, per file format.
func parseLogFile(file domain.LogFile, raw string) []domain.LogEntry {
	regels := strings.Split(raw, "\n")
	entries := make([]domain.LogEntry, 0, len(regels))
	for _, regel := range regels {
		regel = strings.TrimRight(regel, "\r")
		if strings.TrimSpace(regel) == "" {
			continue
		}
		var (
			e  domain.LogEntry
			ok bool
		)
		switch file {
		case domain.LogFileAccess:
			e, ok = parseAccessLine(regel)
		case domain.LogFileCachePerf:
			e, ok = parseCachePerfLine(regel)
		default:
			e, ok = parseErrorLine(regel)
		}
		if !ok {
			e = domain.LogEntry{Kind: domain.KindUnknown, Message: kap(regel, maxTitel), Raw: kap(regel, maxRuweRegel)}
		}
		entries = append(entries, e)
	}
	return entries
}

// parseErrorLine parses one nginx error.log line, unwrapping a PHP message when
// there is one.
func parseErrorLine(regel string) (domain.LogEntry, bool) {
	m := reErrorHead.FindStringSubmatch(regel)
	if m == nil {
		return domain.LogEntry{}, false
	}
	e := domain.LogEntry{
		Level: m[2],
		Raw:   kap(regel, maxRuweRegel),
	}
	if t, err := time.Parse(nginxTijdLayout, m[1]); err == nil {
		e.Time = t.UTC()
	}

	rest := m[3]
	kern := rest
	staart := rest

	if fc := reFastCGI.FindStringSubmatch(rest); fc != nil {
		payload := fc[1]
		staart = fc[2]
		e.Kind, e.Message, e.File, e.Line, e.Stack = ontleedPHPPayload(payload)
	} else {
		// Split the nginx message from its trailing metadata. The metadata
		// always starts at ", client: "; without it the whole rest is message.
		if i := strings.Index(rest, ", client: "); i >= 0 {
			kern = rest[:i]
			staart = rest[i:]
		}
		e.Message = kap(kern, maxTitel*4)
		e.Kind = classificeerNginx(kern)
	}

	if c := reMetaClient.FindStringSubmatch(staart); c != nil {
		e.ClientIP = c[1]
	}
	if r := reMetaRequest.FindStringSubmatch(staart); r != nil {
		e.Request = r[1]
	}
	if h := reMetaHost.FindStringSubmatch(staart); h != nil {
		e.Host = h[1]
	}
	return e, true
}

// ontleedPHPPayload interprets the string PHP wrote to stderr.
func ontleedPHPPayload(payload string) (kind domain.LogKind, melding, bestand string, regelnr int, stack string) {
	if m := rePHPLevel.FindStringSubmatch(payload); m != nil {
		kind = phpKind(m[1])
		body := m[2]

		// The stack trace is noise in the title but valuable context for the AI.
		if i := strings.Index(body, "Stack trace:"); i >= 0 {
			stack = strings.TrimSpace(body[i+len("Stack trace:"):])
			melding = strings.TrimSpace(body[:i])
		} else {
			melding = strings.TrimSpace(body)
		}
		bestand, regelnr = vindBestandEnRegel(body)
		return kind, kap(melding, maxTitel*4), bestand, regelnr, kap(stack, maxRuweRegel)
	}
	if m := rePHPAny.FindStringSubmatch(payload); m != nil {
		bestand, regelnr = vindBestandEnRegel(m[1])
		return domain.KindPHPOther, kap(strings.TrimSpace(m[1]), maxTitel*4), bestand, regelnr, ""
	}
	// stderr from something that is not PHP's error handler at all.
	return domain.KindNginx, kap(strings.TrimSpace(payload), maxTitel*4), "", 0, ""
}

func phpKind(niveau string) domain.LogKind {
	switch niveau {
	case "Fatal error", "Parse error", "Recoverable fatal error":
		return domain.KindPHPFatal
	case "Warning":
		return domain.KindPHPWarning
	case "Deprecated":
		return domain.KindPHPDeprecated
	case "Notice":
		return domain.KindPHPNotice
	}
	return domain.KindPHPOther
}

// vindBestandEnRegel prefers the "thrown in X on line N" form, because for an
// uncaught exception that is where it actually surfaced.
func vindBestandEnRegel(s string) (string, int) {
	for _, re := range []*regexp.Regexp{reThrownIn, reInFileColon, reInFileOnLine} {
		if m := re.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[2])
			return m[1], n
		}
	}
	return "", 0
}

// classificeerNginx separates scanner traffic from real server errors. Being
// conservative here is deliberate: only patterns that are never a code bug are
// labelled bot traffic, everything else stays a plain nginx error.
func classificeerNginx(melding string) domain.LogKind {
	switch {
	case strings.Contains(melding, "directory index of ") && strings.Contains(melding, "is forbidden"):
		return domain.KindBotProbe
	case strings.Contains(melding, "access forbidden by rule"):
		return domain.KindBotProbe
	}
	return domain.KindNginx
}

func parseAccessLine(regel string) (domain.LogEntry, bool) {
	m := reAccess.FindStringSubmatch(regel)
	if m == nil {
		return domain.LogEntry{}, false
	}
	e := domain.LogEntry{
		Kind:     domain.KindAccess,
		Host:     m[1],
		ClientIP: m[2],
		Request:  m[4] + " " + m[5] + " " + m[6],
		Message:  m[7] + " " + m[4] + " " + m[5],
		Raw:      kap(regel, maxRuweRegel),
	}
	if t, err := time.Parse(accessTijdLayout, m[3]); err == nil {
		e.Time = t.UTC()
	}
	return e, true
}

func parseCachePerfLine(regel string) (domain.LogEntry, bool) {
	m := reCachePerf.FindStringSubmatch(regel)
	if m == nil {
		return domain.LogEntry{}, false
	}
	e := domain.LogEntry{
		Kind:     domain.KindAccess,
		ClientIP: m[4],
		Request:  m[5] + " " + m[6] + " " + m[7],
		Message:  m[2] + " " + m[8] + " " + m[5] + " " + m[6],
		Raw:      kap(regel, maxRuweRegel),
	}
	if t, err := time.Parse(accessTijdLayout, m[1]); err == nil {
		e.Time = t.UTC()
	}
	return e, true
}

// groepeerEntries collapses entries that describe the same thing. This is what
// makes the log readable: a real sample of 147 lines held 141 scanner hits that
// differ only in the directory they probed, and they belong in one row.
func groepeerEntries(entries []domain.LogEntry) []domain.LogGroup {
	index := map[string]int{}
	var groepen []domain.LogGroup

	for _, e := range entries {
		titel, sleutel := groepeerSleutel(e)
		id := vingerafdruk(sleutel)
		i, bestaat := index[id]
		if !bestaat {
			groepen = append(groepen, domain.LogGroup{
				ID:       id,
				Kind:     e.Kind,
				Severity: e.Kind.Severity(),
				Title:    titel,
				File:     e.File,
				Line:     e.Line,
				First:    e.Time,
				Last:     e.Time,
			})
			i = len(groepen) - 1
			index[id] = i
		}
		g := &groepen[i]
		g.Count++
		if !e.Time.IsZero() {
			if g.First.IsZero() || e.Time.Before(g.First) {
				g.First = e.Time
			}
			if e.Time.After(g.Last) {
				g.Last = e.Time
			}
		}
		// Keep the newest samples: replace the oldest once the cap is reached.
		if len(g.Samples) < maxVoorbeelden {
			g.Samples = append(g.Samples, e)
		} else if e.Time.After(g.Samples[len(g.Samples)-1].Time) {
			g.Samples[len(g.Samples)-1] = e
		}
	}

	for i := range groepen {
		s := groepen[i].Samples
		sort.SliceStable(s, func(a, b int) bool { return s[a].Time.After(s[b].Time) })
	}
	sorteerGroepen(groepen)
	return groepen
}

// groepeerSleutel returns the display title and the grouping key for an entry.
// PHP messages keep their quoted text — `Undefined constant "ABSPATH"` is the
// identity of the error — while nginx messages have their quoted paths
// generalised so that hundreds of probed directories collapse into one group.
func groepeerSleutel(e domain.LogEntry) (titel, sleutel string) {
	switch {
	case e.Kind.IsPHP():
		titel = kap(e.Message, maxTitel)
		sleutel = string(e.Kind) + "|" + e.File + "|" + strconv.Itoa(e.Line) + "|" +
			normaliseer(kap(e.Message, maxTitel))
	case e.Kind == domain.KindAccess:
		titel = kap(e.Message, maxTitel)
		sleutel = string(e.Kind) + "|" + normaliseer(generaliseerAanhalingen(e.Message))
	default:
		titel = kap(generaliseerAanhalingen(e.Message), maxTitel)
		sleutel = string(e.Kind) + "|" + normaliseer(titel)
	}
	return titel, sleutel
}

func sorteerGroepen(groepen []domain.LogGroup) {
	sort.SliceStable(groepen, func(a, b int) bool {
		if groepen[a].Severity != groepen[b].Severity {
			return groepen[a].Severity > groepen[b].Severity
		}
		if groepen[a].Count != groepen[b].Count {
			return groepen[a].Count > groepen[b].Count
		}
		return groepen[a].Last.After(groepen[b].Last)
	})
}

// generaliseerAanhalingen replaces quoted runs with an ellipsis so that lines
// differing only in a path group together.
func generaliseerAanhalingen(s string) string {
	return reQuotedContent.ReplaceAllString(s, `"…"`)
}

// normaliseer strips the parts that vary between otherwise identical lines.
func normaliseer(s string) string {
	s = reCijfers.ReplaceAllString(s, "N")
	s = reWitruimte.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// vingerafdruk is a short stable id, also used as the branch-name suffix, so it
// must contain nothing but hex.
func vingerafdruk(sleutel string) string {
	sum := sha1.Sum([]byte(sleutel))
	return hex.EncodeToString(sum[:])[:12]
}

func kap(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Trim on a rune boundary so the result stays valid UTF-8.
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
