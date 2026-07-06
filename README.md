# tsuji (辻)

A self-hostable LLM gateway and model marketplace.
One API key, one OpenAI-compatible endpoint, every model behind it.
Tsuji is the crossroads where your requests meet the provider that should serve them.

## What it does

- OpenAI-compatible API at `/api/v1/chat/completions` with streaming, tool calling, structured outputs, and multimodal input
- Routes each request across providers by price, latency, or explicit order, with automatic fallback when a provider fails
- Model catalog with per-provider pricing, context length, throughput, and uptime
- Chat playground for talking to several models side by side
- API keys, credits, usage accounting, and activity history
- Bring your own provider keys or let tsuji hold shared upstream keys

## Status

Early. The plan of record lives in milestone checklists and gets ticked as code lands.

## Build

```sh
go build ./cmd/tsuji
./tsuji serve
```

## License

MIT
