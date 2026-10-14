package ziplogreceiver

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func writeZip(t *testing.T, path string, entries [][2]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(e[1]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func startReceiver(t *testing.T, mutate func(*Config)) *consumertest.LogsSink {
	t.Helper()
	f := NewFactory()
	cfg := f.CreateDefaultConfig().(*Config)
	cfg.PollInterval = 50 * time.Millisecond
	mutate(cfg)
	require.NoError(t, cfg.Validate())

	sink := new(consumertest.LogsSink)
	r, err := f.CreateLogs(context.Background(), receivertest.NewNopSettings(f.Type()), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, r.Shutdown(context.Background())) })
	return sink
}

func records(sink *consumertest.LogsSink) []plog.LogRecord {
	var out []plog.LogRecord
	for _, ld := range sink.AllLogs() {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				for k := 0; k < sl.LogRecords().Len(); k++ {
					out = append(out, sl.LogRecords().At(k))
				}
			}
		}
	}
	return out
}

func TestReceiverEmitsZipEntryRecordsExactlyOnceAcrossPolls(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"app/x.log", "one\ntwo\nthree\n"}})

	sink := startReceiver(t, func(c *Config) { c.Include = []string{filepath.Join(dir, "*.zip")} })

	require.Eventually(t, func() bool { return sink.LogRecordCount() == 3 }, 5*time.Second, 20*time.Millisecond)
	time.Sleep(400 * time.Millisecond) // several more polls; done entries must not be re-read
	require.Equal(t, 3, sink.LogRecordCount())

	rs := records(sink)
	require.Equal(t, "one", rs[0].Body().AsString())
	name, _ := rs[0].Attributes().Get("log.file.name")
	require.Equal(t, "a.zip", name.AsString())
	entry, _ := rs[0].Attributes().Get("log.zip.entry")
	require.Equal(t, "app/x.log", entry.AsString())
}

func TestReceiverRunsConfiguredStanzaOperatorsOnRecords(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "j.zip"), [][2]string{{"j.log", `{"level":"error","msg":"boom"}` + "\n"}})

	sink := startReceiver(t, func(c *Config) {
		c.Include = []string{filepath.Join(dir, "*.zip")}
		// Load operators the way the collector would: through confmap.
		conf := confmap.NewFromStringMap(map[string]any{
			"operators": []any{map[string]any{"type": "json_parser"}},
		})
		require.NoError(t, conf.Unmarshal(c))
	})

	require.Eventually(t, func() bool { return sink.LogRecordCount() == 1 }, 5*time.Second, 20*time.Millisecond)
	msg, ok := records(sink)[0].Attributes().Get("msg")
	require.True(t, ok, "json_parser should have lifted msg into attributes")
	require.Equal(t, "boom", msg.AsString())
}

func TestReceiverSetsRecordTimestampSoBackendsThatRequireOneAcceptTheLogs(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), [][2]string{{"x.log", "no timestamp in this line\n"}})

	sink := startReceiver(t, func(c *Config) { c.Include = []string{filepath.Join(dir, "*.zip")} })

	require.Eventually(t, func() bool { return sink.LogRecordCount() == 1 }, 5*time.Second, 20*time.Millisecond)
	require.NotZero(t, records(sink)[0].Timestamp(), "Timestamp must default to the observed time when no operator parses one")
}
