# Cost Optimization Guide

This document covers cost analysis and optimization for Logwatch AI Analyzer.

## Anthropic Claude (Cloud) Costs

### Cost accounting

The analyzer calculates each run from the input and output usage returned by
Anthropic and the model-aware pricing table. It does not currently send cache
control blocks, so budget estimates must use normal input pricing. Cached-token
fields remain in storage and reporting for forward compatibility but are not
advertised as an active optimization.

### Cost Reduction Strategies

1. Increase `MAX_PREPROCESSING_TOKENS` compression
2. Reduce historical context days (currently 7)
3. Adjust section priority classification
4. Use smaller model (not recommended - quality drop)

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
OLLAMA_CONTEXT_TOKENS=32768
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
LMSTUDIO_CONTEXT_TOKENS=32768
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
