# zip_log: read logs from zip files with Bindplane

`zip_log` is an OpenTelemetry Collector receiver that reads log lines from inside `.zip` files, the way a Splunk Universal Forwarder does. Use it to backfill archived logs. Bindplane manages the collector that runs it.

Each zip entry becomes log records. Progress is saved per entry, so a finished entry is never read twice and a restart picks up where it stopped.

## What you need

- Bindplane Enterprise (Bring Your Own Collector) and an org admin login
- Go and the `builder` tool (OCB) v0.161.0: `go install go.opentelemetry.io/collector/cmd/builder@v0.161.0`
- A GitHub repo where the built collector is published as releases
- The `bindplane` CLI, logged in to the customer's project

## 1. Build the collector

The build starts from Bindplane's own component list, so every Bindplane source and destination works, then adds `zip_log`.

```
cd distro
./generate-manifest.sh v1.109.0
builder --config=builder.yaml
```

By default the script uses `../ziplogreceiver`. Pass a different path as a second argument if the receiver lives elsewhere. The binary is `distro/build/zip-log-collector`. It takes about 5 minutes. The version argument is a Bindplane collector release. The collector and component versions in the manifest follow it, so use the latest release when you start a new customer.

To publish to GitHub, run `builder.yaml` through Bindplane's `observiq/otel-distro-builder` action. It builds each platform, packages the OpAMP supervisor, and attaches everything to a release.

## 2. Register the collector type in Bindplane

Edit `distro/agent-type.yaml` and set `repositoryLink` to your releases repo. Then:

```
bindplane apply -f distro/agent-type.yaml
bindplane sync agent-versions --agent-type zip-log-collector
```

`metadata.name` must match `dist.name` in `builder.yaml`.

## 3. Install the collector on a host

Run the install command Bindplane gives you:

```
bindplane install agent --agent-type zip-log-collector --platform linux
```

The collector runs under the OpAMP supervisor, which connects to Bindplane. Two environment variables must be set for the collector process, because Bindplane's generated configs use them for storage paths. The install script sets them. If you start the supervisor yourself, set both:

```
BINDPLANE_COLLECTOR_HOME=/path/to/home
OIQ_OTEL_COLLECTOR_HOME=/path/to/home
```

If either is missing, the collector exits at startup with `mkdir /storage: read-only file system`, and Bindplane shows `RemoteConfigError`.

## 4. Add the source and a configuration

```
bindplane apply -f distro/bindplane/zip_log-source-type.yaml
```

This adds a "Zip Archive Logs" source. Copy `distro/bindplane/zip-test-configuration.yaml`, set the zip path and your destination, apply it, assign the agent, and roll it out:

```
bindplane apply -f my-config.yaml
bindplane label agent <AGENT_ID> configuration=my-config --overwrite
bindplane rollout start my-config
```

You can also build the configuration in the Bindplane console. Pick the new source.

If the source type can't be applied, paste `distro/bindplane-custom-source.yaml` into Bindplane's built-in Custom source instead.

## Source settings

| Setting | Default | Meaning |
|---|---|---|
| Zip file paths | required | Globs for zip files. `**` works. |
| Exclude paths | none | Zip files to skip. |
| Entry include / exclude | all | Globs matched against names inside each zip, like `**/*.log`. |
| Multiline start pattern | none | Regex for the first line of a record. Empty means one line per record. |
| Encoding | utf-8 | Also utf-16le, utf-16be, ascii, nop. |
| Poll interval | 5 s | How often to look for new zips. |
| Max uncompressed entry size | 0 (off) | Skips entries larger than this when unzipped. |
| Max compression ratio | 0 (off) | Skips entries compressed more than this. Guards against zip bombs. |
| Progress storage | on | Saves progress to disk. Turn it off and a restart reads everything again. |

Each record has `log.file.name` (the zip), `log.file.path`, `log.zip.entry`, and the timestamp of when it was read. To use the time in the log line, add a Bindplane processor that parses it.

## Things to know

- Changing a source in Bindplane gives it a new ID, which starts progress over. The zips are read again. Expect duplicates when you edit and re-apply a source after data has flowed.
- A zip copied to a new path is read again.
- A bad zip or an entry over a limit is logged and skipped. The other files continue.
- After a failed push, Bindplane keeps showing the error until a new config version rolls out. Change something in the config, apply it, and run `rollout start`.
- Not supported: password-protected zips, zips that grow after they are first read, zips inside zips, tar and gz, deleting or moving zips after reading.

## What was tested, and what wasn't

Tested on macOS (arm64) against Bindplane Cloud: build, supervisor connection, config rollout, 13,283 records from three zips arriving in Dynatrace with correct counts per entry, no re-reads after a config change, new zips picked up.

Not tested: the GitHub release build and `bindplane install agent` path (the collector was started by hand with the supervisor), Linux and Windows, a crash in the middle of a zip, zip64, UTF-16 end to end, files over 1 GB.

## Code

- `ziplogreceiver/` is the Go receiver. Run `go test -race ./...` there.
- `distro/` has the manifest generator and the Bindplane resources.
