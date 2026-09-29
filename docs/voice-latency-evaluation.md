# Voice latency check — 2026-09-11

Brain T previously used `anthropic/claude-sonnet-4` through OpenRouter. Four
interleaved live requests used the same read-only prompt: find the seeded
“Start here — mobile playground” note and return its path with one short
sentence. Requests ran through Brain's actual assistant stream endpoint,
with `voice: true` and a per-request model override.

| Model | First text (seconds) | Complete reply (seconds) |
|---|---|---|
| Sonnet 4, first check | 6.27 | 7.27 |
| Haiku 4.5, first check | 3.83 | 4.36 |
| Sonnet 4, second check | 5.67 | 6.28 |
| Haiku 4.5, second check | 1.84 | 4.54 |

All four invoked `recall` and returned the correct seeded path
`projects/mobile-playground/scratch/g880xps0.md`. This is a small smoke comparison,
not a broad quality evaluation or latency guarantee. Haiku 4.5 is selected for
Brain T only. Production model configuration is unchanged.

The client still waits for the complete reasoning reply, then requests TTS and
loads its entire audio response. These numbers exclude transcription and TTS.
Sentence-level speech streaming is not implemented by this change.

## Breeze on AMOS

AMOS reports Tesla P40, 24 GB VRAM, compute capability 6.1, driver 535.54.03,
11 GiB host RAM and 63 GB free disk. Memory capacity alone does not establish
compatibility or speed.

Reviewed upstream Breeze commit `008f769016b0a24711becd7a4925030bc93f608c`:

- Its Dockerfile uses PyTorch 2.9.1 / CUDA 12.8 and builds FlashAttention 2.8.3
  for Hopper by default. That stock build is not compatible with the P40.
- The API itself defaults to eager attention, but its model loader hardcodes
  BF16. A P40 experiment would need a Pascal-compatible PyTorch build and a
  validated precision fallback, not merely a different Docker GPU flag.
- No Breeze weights were downloaded, container started, or latency measured.
  The existing Kokoro provider remains configured. A compatibility port would
  need actual generation, quality and latency tests before selecting it.

Sources: [Breeze source](https://github.com/breezeblue-ai/breeze-tts),
[PyTorch CUDA architecture change](https://dev-discuss.pytorch.org/t/cuda-toolkit-version-and-architecture-support-update-maxwell-and-pascal-architecture-support-removed-in-cuda-12-8-and-12-9-builds/3128),
[FlashAttention supported GPUs](https://github.com/Dao-AILab/flash-attention).
