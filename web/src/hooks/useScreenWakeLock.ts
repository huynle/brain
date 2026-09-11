import { useEffect, useState } from 'react';

/** Keep the display awake only for the current hands-free session. */
export function useScreenWakeLock(enabled: boolean) {
  const [message, setMessage] = useState('');
  useEffect(() => {
    if (!enabled) { setMessage(''); return; }
    let disposed = false;
    let lock: WakeLockSentinel | undefined;
    const unavailable = () => {
      if (!disposed) setMessage('Screen may sleep — keep Brain visible.');
    };
    const released = () => unavailable();
    setMessage('Keeping screen awake…');
    if (!navigator.wakeLock) unavailable();
    else void navigator.wakeLock.request('screen').then(async acquired => {
      if (disposed) { await acquired.release(); return; }
      lock = acquired;
      lock.addEventListener('release', released);
      if (lock.released) unavailable();
      else setMessage('Screen kept awake');
    }).catch(unavailable);
    return () => {
      disposed = true;
      lock?.removeEventListener('release', released);
      void lock?.release().catch(() => {});
    };
  }, [enabled]);
  return enabled ? message : '';
}
