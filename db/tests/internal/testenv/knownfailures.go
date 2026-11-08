package testenv

import (
	"flag"
	"io"
	"os"
	"regexp"
	"strings"
)

// KnownFailure is a test that fails against one source for a reason outside Nanostack DB:
// a database server behaviour that differs from what the test expects. The harness
// leaves it out of the normal run, then runs it alone and requires it to fail with
// output that contains Signature, the exact wrong result. So the rest of the suite stays
// green, the failure stays pinned, and a server or test fix makes the run fail with
// "now passes: remove it", never silently.
type KnownFailure struct {
	Suite  string // package, e.g. "postgres"
	Source Source
	Test   string // top-level test function
	// Signature is text the failure output must contain: the actual value the test saw.
	Signature string
	Reason    string
}

// knownFailures is the complete list. Each entry has a row in db/UPSTREAM.md.
var knownFailures = []KnownFailure{
	{
		Suite:  "postgres",
		Source: Postgres18,
		Test:   "TestSelectGroupByCube",
		// testutils.AssertJSON prints the query result as "expected" and the test's
		// expectation as "actual". The result is in the correct ORDER BY country, city order.
		Signature: `expected: "\n[\n\t{\n\t\t\"Country\": \"Belarus\",\n\t\t\"City\": \"Mogiljov\",\n\t\t\"Count\": 1\n\t},\n\t{\n\t\t\"Country\": \"Belarus\",\n\t\t\"City\": \"Molodetno\",\n\t\t\"Count\": 1\n\t},\n\t{\n\t\t\"Country\": \"Belarus\",\n\t\t\"City\": \"\",\n\t\t\"Count\": 2\n\t},\n\t{\n\t\t\"Country\": \"\",\n\t\t\"City\": \"Mogiljov\",\n\t\t\"Count\": 1\n\t},\n\t{\n\t\t\"Country\": \"\",\n\t\t\"City\": \"Molodetno\",\n\t\t\"Count\": 1\n\t},\n\t{\n\t\t\"Country\": \"\",\n\t\t\"City\": \"\",\n\t\t\"Count\": 2\n\t}\n]\n"`,
		Reason: "the expected rows record PostgreSQL 14-17 output, which is not in ORDER BY order: WHERE country = 'Belarus' " +
			"makes the planner drop country from the sort key although GROUP BY CUBE sets it to NULL in some rows. " +
			"PostgreSQL 18 sorts by country, city as asked. Candidate upstream PR: order-independent expectation",
	},
	{
		Suite:  "mysql",
		Source: MySQL,
		Test:   "TestCast",
		// The whole result, as AssertDeepEqual prints it: only Time is wrong.
		Signature: "Actual: \n{test 2011-02-02 00:00:00 +0000 UTC 0000-01-01 00:00:00 +0000 UTC 2011-02-02 14:06:10 +0000 UTC 150 150 -5 15 Some text}",
		Reason: "MySQL 8.4 returns 00:00:00 for CAST(? AS TIME) when the parameter of a server-side prepared statement " +
			"is the string '14:06:10' (MySQL 8.0.27, which upstream tests, and MariaDB return 14:06:10; " +
			"CAST('14:06:10' AS TIME) and CAST(CAST(? AS CHAR) AS TIME) are correct on 8.4). The SQL and argument jet sends are right",
	},
}

// knownFailuresFor returns the entries of suite against src.
func knownFailuresFor(suite string, src Source) []KnownFailure {
	var out []KnownFailure
	for _, k := range knownFailures {
		if k.Suite == suite && k.Source == src {
			out = append(out, k)
		}
	}
	return out
}

// runWithKnownFailures runs the suite with the known failures of env's source skipped,
// then runs each of them alone and checks that it still fails the known way.
func (h *harness) runWithKnownFailures(env *Env) int {
	known := knownFailuresFor(h.suite.Name, env.Source)
	if len(known) == 0 {
		return h.suite.Run(env)
	}

	userRun, userSkip := flagValue("test.run"), flagValue("test.skip")
	if strings.Contains(userSkip, "/") {
		// testing splits -skip at "/" into per-level patterns, so the known failures cannot
		// be appended to it: run them like any test, failing as they fail.
		progressf("testenv: %s: -skip=%q has a subtest pattern; the known failures of %s run as ordinary tests\n",
			h.suite.Name, userSkip, env.Source)
		return h.suite.Run(env)
	}
	defer func() {
		_ = flag.Set("test.run", userRun)
		_ = flag.Set("test.skip", userSkip)
	}()

	names := make([]string, len(known))
	for i, k := range known {
		names[i] = regexp.QuoteMeta(k.Test)
	}
	skip := "^(" + strings.Join(names, "|") + ")$"
	if userSkip != "" {
		skip = userSkip + "|" + skip
	}
	_ = flag.Set("test.skip", skip)
	code := h.suite.Run(env)

	for _, k := range known {
		if !selectedByUser(k.Test, userRun, userSkip) {
			continue
		}
		_ = flag.Set("test.run", "^"+regexp.QuoteMeta(k.Test)+"$")
		_ = flag.Set("test.skip", "")
		var c int
		out := captureStdout(func() { c = h.suite.Run(env) })
		switch {
		case c == 0:
			progressf(
				"testenv: %s: FAIL: known failure %s against %s now passes; remove it from knownFailures and its UPSTREAM.md row\n",
				h.suite.Name,
				k.Test,
				env.Source,
			)
			code = 1
		case !strings.Contains(out, k.Signature):
			progressf(
				"testenv: %s: FAIL: known failure %s against %s fails differently than recorded (want output containing %q):\n%s\n",
				h.suite.Name,
				k.Test,
				env.Source,
				k.Signature,
				out,
			)
			code = 1
		default:
			progressf(
				"testenv: %s: known failure %s against %s still fails as recorded: %s\n",
				h.suite.Name,
				k.Test,
				env.Source,
				k.Reason,
			)
		}
	}
	return code
}

func flagValue(name string) string {
	if f := flag.Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}

// selectedByUser reports whether the user's -run and -skip patterns select the top-level
// test name (the first element of each slash-separated pattern).
func selectedByUser(name, run, skip string) bool {
	match := func(pattern string) bool {
		first, _, _ := strings.Cut(pattern, "/")
		re, err := regexp.Compile(first)
		return err == nil && re.MatchString(name)
	}
	if run != "" && !match(run) {
		return false
	}
	return skip == "" || !match(skip)
}

// captureStdout runs f with os.Stdout redirected and returns what f wrote.
func captureStdout(f func()) string {
	r, w, err := os.Pipe()
	if err != nil {
		f()
		return ""
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() {
		os.Stdout = saved
	}()
	f()
	os.Stdout = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
