# Isolated analysis worker

This worker is the first concrete provider adapter for the isolated engine
protocol. It makes one OpenAI-compatible chat-completions request from one
frozen input bundle and returns the provider JSON to the Go process for strict
contract validation and persistence.

It is deliberately **not** a claim that TradingAgents, a particular model, or
the model's self-reported probability is accurate. The Go application remains
authoritative for input hashes, scoring, budgets, output validation, snapshots,
and seven-trading-day reviews.

## Required environment

- `MODEL_API_ENDPOINT`: complete HTTPS chat-completions URL;
- `MODEL_API_KEY`: provider credential;
- `MODEL_ID`: exact model ID also configured by the Go client;
- `MODEL_INPUT_USD_PER_MILLION`: non-negative input-token price;
- `MODEL_OUTPUT_USD_PER_MILLION`: non-negative output-token price;
- `MODEL_HTTP_TIMEOUT_SECONDS`: optional HTTP timeout, default `60`.

`ALLOW_HTTP_LOOPBACK=1` permits HTTP only for loopback integration tests. It
does not permit an arbitrary plaintext remote endpoint.

Do not pass database, GitHub, TuShare, or desktop-process credentials to the
worker. The Go `EngineProcessPolicy` environment allowlist should contain only
the model variables above, and its artifact manifest should pin both the Python
executable and `worker.py` by SHA-256.

## Protocol behavior

The worker:

1. reads exactly one bounded JSON request from standard input;
2. checks protocol, bundle schema, hash shape, model, and prompt identity;
3. sends the frozen bundle as untrusted evidence under a fixed system prompt;
4. requests JSON-only output with deterministic temperature;
5. calculates actual request cost from provider token usage and explicitly
   configured per-million-token prices, allowing the Go budget boundary to
   settle the prior reservation;
6. writes no secrets or provider response bodies to stderr.

The worker has no third-party Python dependencies. Run its tests with:

```bash
python3 -m unittest -v backend/analysisworker/test_worker.py
```
