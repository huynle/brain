import * as ort from 'onnxruntime-web/wasm';
import { SileroV5 } from '@ricky0123/vad-web/dist/models/v5';
import { Resampler } from '@ricky0123/vad-web/dist/resampler';

/** Model and runtime are served locally; no microphone audio leaves this detector. */
export async function createSpeechDetector(rate: number) {
  ort.env.wasm.wasmPaths = '/assets/voice/v5-0.0.30-ort1.29.0/';
  ort.env.wasm.numThreads = 1;
  ort.env.wasm.proxy = false;
  const model = await SileroV5.new(ort, async () => {
    const response = await fetch('/assets/voice/v5-0.0.30-ort1.29.0/silero_vad_v5.onnx');
    if (!response.ok) throw new Error('Speech detector model unavailable');
    return response.arrayBuffer();
  });
  const resampler = new Resampler({ nativeSampleRate: rate, targetSampleRate: 16000, targetFrameSize: 512 });
  let closed = false;
  let pending: Promise<void> = Promise.resolve();
  return {
    process(pcm: Float32Array, onFrame: (frame: Float32Array, probability: number) => void) {
      pending = pending.then(async () => {
        if (closed) return;
        for (const frame of resampler.process(pcm)) {
          if (closed) return;
          const probability = await model.process(frame);
          if (!closed) onFrame(frame, probability.isSpeech);
        }
      });
      return pending;
    },
    async close() {
      closed = true;
      await pending.catch(() => {});
      await model.release();
    },
  };
}
