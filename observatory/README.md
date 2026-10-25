# TokenTelemetry Observatory for Windows

A local browser dashboard and Ollama measurement proxy built on
https://github.com/semyonfox/tokentelemetry at commit
`238ef8bc81bfba4c80bcd97a5fffd8c8d4a5df53`. The original MIT license and
accounting engine are retained. This addition has no hosted backend, analytics,
JavaScript package dependencies, or browser downloads.

## Install and run

Unzip the Windows bundle, then run in PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\Install.ps1
```

Open **TokenTelemetry Observatory** from the Windows Start menu. It starts a
hidden local process and opens http://127.0.0.1:8787 in your browser. Use
**Stop Observatory** in Start to stop it and its SSH tunnel. Closing the browser
does not stop the process. There is no automatic launch at sign-in.

Alternatively run `Launch.ps1` directly from the unzipped portable bundle.
The Windows x64 executable needs neither Go nor Node installed. Windows OpenSSH
Client is needed only for SSH connections. This is a locally built, unsigned
application, not a Microsoft Store or signed installer.

Installation: `%LOCALAPPDATA%\Programs\TokenTelemetryObservatory`.
Configuration, numeric measurements and server log:
`%LOCALAPPDATA%\TokenTelemetryObservatory`. User-specific hosts are not shipped
in the source or portable bundle. Installation preserves an existing config.
For another data directory/port, run:

```powershell
.\observatory.exe -data-dir C:\path\to\data -port 8788
```

The launcher and Start shortcuts use port 8787. Stop the app before updating its
executable. To uninstall, stop it and remove its installation folder and Start
menu folder. Keep the separate data folder to preserve history, or remove it
yourself to erase configuration and captured counters.

## Connect the Mac Mini

Recommended: use an existing SSH connection over your LAN or Tailscale. In
PowerShell, first verify that `ssh your-user@your-mac` works and trust the
correct host key yourself. Configure key authentication if it asks for a
password. Host aliases, keys and nonstandard ports belong in your SSH config.
The app uses `BatchMode=yes` and `StrictHostKeyChecking=yes` and never saves
passwords or accepts unknown host keys.

Choose **SSH tunnel to Mac** in Connection & thresholds, enter the destination,
and Save & reconnect. The app forwards laptop `127.0.0.1:11435` to the Mac's
`127.0.0.1:11434`. Ollama must already be running. It does not change the Mac's
firewall, Ollama binding, SSH configuration or model state. An exited tunnel
is reported; after restoring connectivity, choose Save & reconnect. Port 11435
must be available. Direct mode accepts an existing reachable HTTP(S) origin;
authenticated remote API gateways are not supported by this first version.

An optional SSH destination in direct mode still enables Mac host metrics.
CPU and physical memory are sampled with fixed, read-only `top` and `sysctl`
commands, no more frequently than every 15 seconds. These describe the whole
Mac, including other workloads. No remote agent or root privileges are needed.

## Measure token usage

Configure your laptop application's native Ollama base URL to
`http://127.0.0.1:8787`. The proxy supports `/api/chat`, `/api/generate` (both
streaming and nonstreaming), `/api/tags`, `/api/ps`, `/api/show` and
`/api/version`. OpenAI-compatible `/v1`, embeddings, model pull, unload/delete
management interfaces and arbitrary API routes are not supported.

Example, after selecting an installed model (this deliberately runs inference):

```powershell
$body = @{model='YOUR_INSTALLED_MODEL';prompt='Reply with one short sentence.';stream=$false;options=@{num_predict=32;num_ctx=4096}} | ConvertTo-Json
Invoke-RestMethod http://127.0.0.1:8787/api/generate -Method Post -ContentType application/json -Body $body
```

The proxy records numeric counters from Ollama's terminal response, model ID,
timestamp, status and requested context. Prompts and responses pass through
memory and are not saved. HTTP errors, interrupted streams and absent counters
remain explicitly unmeasured. Request limit: 16 MiB; runtime: 30 minutes;
individual JSON response records over 4 MiB are forwarded but not measured.
The last 10,000 requests are retained atomically in `measurements.json`; this
is a bounded recent ledger, not a complete lifetime archive. No raw token
counts are invented. Observations survive restart; in-flight counters do not.

Requests made directly to Ollama, including requests from other devices, are
not visible to the token ledger. Installed and loaded models can still be
polled. A loaded model is not necessarily active. “Requests in flight” counts
only requests through this proxy.

## Understand the measurements

- **Historical tokens:** totals of measured input and output over retained
  requests. Input may contain earlier conversation text again; these are
  processed tokens, not unique words. Cumulative tokens are never presented as
  a local Ollama quota.
- **Context:** loaded runtime context capacity comes from `/api/ps`, when
  supplied. Live context occupancy is unavailable. A warning can compare the
  last prompt count with that same request's explicit `options.num_ctx`; it
  cannot prove server-enforced capacity, remaining slots or truncation.
- **Speed:** measured output tokens divided by Ollama's decode duration, not
  whole-request latency. Model-level speed is weighted by decode time.
- **CPU/memory:** whole-host readings; macOS used memory includes reclaimable
  caches and compressed pages and is not a memory-pressure measure.
- **GPU:** utilization is unavailable. `size_vram` is loaded GPU memory
  allocation, not utilization or a complete Apple unified-memory breakdown.
- **Switch suggestions:** configurable speed, last-prompt/window and CPU
  thresholds explain their evidence. Request suggestions use only the latest
  completed request per model from the last 15 minutes. No automatic switch,
  quality score, hardware-based quality claim, or untested alternative-model
  recommendation is made.

## Laptop agent accounting

Choose **Read local agent logs** to run the original engine's
`summary --json` over its default last-30-local-calendar-day window. It reads
existing laptop agent logs/databases using the upstream adapters. Warnings,
unpriced counts, billing classes, exclusions and the full JSON remain visible.
No Mac agent logs are scanned. Missing logs produce an unavailable state.
The dashboard caches this report for 60 seconds and imposes a 45-second
timeout; use the CLI for unusually large histories:

```powershell
.\observatory.exe accounting summary --json
.\observatory.exe accounting agents --installed
```

The dashboard sets `TT_OFFLINE=1` for this subprocess. Upstream scan caches can
still be written in their normal cache directory, but original source logs
are never modified. API list value is not actual spend or an invoice; local,
subscription and unpriced usage remain classified by the upstream engine.
Provider plan windows appear only when supplied by source logs. They show
their observation and reset times and are not live provider API queries.
Do not add agent accounting to proxy totals: they can describe the same work.

## Development and verification

Source is under `engine/internal/observatory` and `engine/cmd/observatory`.
Requires Go >=1.26 to build, matching upstream:

```powershell
cd engine
go test ./...
go vet ./...
cd ..
powershell -NoProfile -ExecutionPolicy Bypass -File observatory\build.ps1
```

The build produces the portable bundle plus the unchanged `tokentelemetry`
and `tt` CLI executables. Tests use synthetic local HTTP servers and temporary
storage, never personal agent logs. Integration tests cover streaming fidelity,
measured counters, nulls/errors, persistence, privacy, origin/host guards,
connection failure, macOS parsing and threshold suggestions. Real Mac GPU
sampling and model quality evaluation are outside the supported scope.

Reference API semantics: https://docs.ollama.com/api/ps,
https://docs.ollama.com/api/generate, https://docs.ollama.com/api/chat,
https://docs.ollama.com/faq. Endpoints and fields can change with Ollama versions;
unsupported data remains unavailable.

## All sources (laptop + Mac)

The **All sources** panel runs the upstream accounting engine on each machine and shows one table of
measured tokens per machine, tool, model, provider and billing class for the last 30 local days, plus
any quota or rate-limit windows that a tool wrote to its own logs (marked fresh only within 15 minutes).
A coverage table lists every supported tool as found, detected with no recent usage, opt-in, or not found.

* Laptop: always available. `observatory.exe collect` runs read-only and offline.
* Mac: in setup, save the SSH destination, click **Install Mac collector** (copies a read-only
  `collector` binary to `~/.local/share/tokentelemetry-observatory/` over your existing SSH key), then
  tick **Read Mac agent history over SSH** and save. Only aggregates are returned: no prompts,
  transcripts, paths or session IDs.
* Not observable: web/app chats (ChatGPT, Claude.ai, Gemini), and quota for subscriptions whose
  tools do not log it. Routers such as OmniRoute and T3 Code are not engine sources; usage made
  through them appears only where the underlying agent logs it. Cloud CPU/GPU is never shown.
