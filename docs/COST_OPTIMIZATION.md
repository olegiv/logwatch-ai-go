# Cost Optimization Guide

This document covers cost analysis and optimization for Logwatch AI Analyzer.

## Anthropic Claude (Cloud) Costs

### Haiku 5.5 Pricing

`claude-haiku-5-5` is the runtime and sample-configuration default. Rates
below are USD per million tokens, using the 5-minute cache-write tier.

| Total prompt tokens | Input | Output | Cache write | Cache read |
|---------------------|------:|-------:|------------:|-----------:|
| Up to 100,000       | $0.10 | $0.50  | $0.125      | $0.01      |
| Over 100,000        | $0.50 | $2.50  | $0.625      | $0.05      |

The selected tier applies to the entire request. Prompt length includes
uncached input, cache writes and cache reads; output does not select the
tier. `ModelPricing.Cost()` uses these usage counts for stored costs and
Telegram reports. See [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing).

### Daily Cost Examples

These are uncached examples, not estimates of every production report.
Output counts include thinking tokens; multiply by the number of daily jobs.

| Input / output tokens per run | Per run | 30 daily runs | 365 daily runs |
|-------------------------------|--------:|--------------:|---------------:|
| 10,000 / 2,000                 | $0.002  | $0.06         | $0.73          |
| 150,000 / 2,000                | $0.08   | $2.40         | $29.20         |

### Prompt Caching Behavior

The current Go client sends no `cache_control`, so it does not request
prompt caching. It tracks cache creation/read usage if returned by the API.
Do not assume consecutive or daily jobs receive cache discounts. Enabling
cache requests would require a separate client change.

### Token and Thinking Budgets

Haiku 5.5 uses a newer tokenizer, which produces approximately 30% more
tokens for the same text than Haiku 4.5. Use the model's token-counting API
and actual usage rather than reusing old counts. The analyzer already counts
the assembled Anthropic prompt when fitting it to the input budget.

Adaptive thinking is on by default. `AI_MAX_TOKENS=8000` covers thinking
plus the JSON response, so a small budget can leave an incomplete answer
or no text at all. If that happens, increase the setting up to the supported
16,000 limit and inspect the next result. The client uses the model's default
effort and exposes no effort configuration. See the
[Haiku 5.5 migration guide](https://platform.claude.com/docs/en/models/haiku-5-5/migration-guide).

### Cost Reduction Strategies

1. Lower `MAX_PREPROCESSING_TOKENS` to trigger compression earlier; verify
   that the resulting report still preserves important events. This setting
   is an estimate for log content, not a guarantee that the full prompt is
   below the 100,000-token pricing threshold.
2. Review the amount of historical context included (currently 7 days).
3. Adjust section priority classification only after checking retained findings.
4. Compare actual token usage and costs across representative reports before
   choosing a different model.

## Ollama (Local) - Zero Cost

For development or cost-sensitive deployments, use Ollama for **free local inference**:

```bash
# Install Ollama (macOS)
brew install ollama

# Pull recommended model (requires ~40GB disk, ~45GB RAM)
ollama pull llama3.3:latest

# Or use a smaller model for lower-RAM systems
ollama pull llama3.2:8b

# Start Ollama server
ollama serve
```

Configure in `.env`:
```
LLM_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.3:latest
```

### Trade-offs

| Pros | Cons |
|------|------|
| Zero cost - unlimited analysis | Slower than cloud |
| Data privacy - logs never leave your machine | Quality varies by model |
| No rate limits | Requires powerful hardware |

## LM Studio (Local) - Zero Cost

LM Studio provides a user-friendly desktop application for running local LLMs with an OpenAI-compatible API.

### Setup

1. Download and install LM Studio from https://lmstudio.ai
2. Download a model from the Search tab
3. Load the model (click on it, then "Load")
4. Enable "Local Server" mode in the left sidebar
5. Server starts on `http://localhost:1234` by default

Configure in `.env`:
```
LLM_PROVIDER=lmstudio
LMSTUDIO_BASE_URL=http://localhost:1234
LMSTUDIO_MODEL=local-model
```

### Recommended Models

| Model | VRAM | Quality | Speed |
|-------|------|---------|-------|
| Llama-3.3-70B-Instruct | ~40GB | Excellent | Slower |
| Qwen2.5-32B-Instruct | ~20GB | Excellent | Medium |
| Mistral-Small-24B-Instruct | ~15GB | Good | Medium |
| Phi-4-14B | ~9GB | Good | Faster |
| Llama-3.2-8B-Instruct | ~5GB | Acceptable | Fast |

**Tip:** Look for GGUF quantized versions (Q4_K_M, Q5_K_M) for better VRAM efficiency.

### Trade-offs

| Pros | Cons |
|------|------|
| Zero cost - unlimited analysis | Slower than cloud |
| Data privacy | Quality varies by model |
| User-friendly GUI | Requires powerful hardware |
| Easy model switching | |
| OpenAI-compatible API | |

## Cost Monitoring

Query costs from the database:

```bash
# Total costs
sqlite3 data/summaries.db "SELECT SUM(cost_usd) FROM summaries;"

# Costs by day
sqlite3 data/summaries.db "SELECT DATE(timestamp), SUM(cost_usd) FROM summaries GROUP BY DATE(timestamp) ORDER BY DATE(timestamp) DESC LIMIT 30;"

# Average cost per run
sqlite3 data/summaries.db "SELECT AVG(cost_usd) FROM summaries WHERE cost_usd > 0;"
```

Use the `/cost-report` slash command for a comprehensive cost analysis.
