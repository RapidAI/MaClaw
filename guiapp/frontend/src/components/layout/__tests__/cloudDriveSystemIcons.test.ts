import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  forgetCloudDriveSystemIcon,
  loadCloudDriveSystemIcon,
  peekCloudDriveSystemIcon,
  resetCloudDriveSystemIconCacheForTests,
} from '../cloudDriveSystemIcons';

type IconCall = { name: string };

function installHostIcons(impl: (name: string) => Promise<string> | string) {
  const calls: IconCall[] = [];
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        AIAssistantFileTypeIconDataURLIfAssociated: async (name: string) => {
          calls.push({ name });
          return impl(name);
        },
      },
    },
  };
  return calls;
}

describe('cloud drive system icons', () => {
  afterEach(() => {
    resetCloudDriveSystemIconCacheForTests();
    delete (window as unknown as { go?: unknown }).go;
  });

  it('asks the host once per suffix and shares the answer across files', async () => {
    const calls = installHostIcons(() => 'data:image/png;base64,PDFICON');

    const [first, second] = await Promise.all([
      loadCloudDriveSystemIcon('.pdf'),
      loadCloudDriveSystemIcon('.pdf'),
    ]);

    expect(first).toEqual({ state: 'icon', url: 'data:image/png;base64,PDFICON' });
    expect(second).toEqual(first);
    expect(calls).toHaveLength(1);
    expect(peekCloudDriveSystemIcon('.pdf')).toBe('data:image/png;base64,PDFICON');
    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual(first);
    expect(calls).toHaveLength(1);

    await loadCloudDriveSystemIcon('.docx');
    expect(calls).toHaveLength(2);
  });

  it('asks the host for a type name, not a local path', async () => {
    const calls = installHostIcons(() => 'data:image/png;base64,X');
    await loadCloudDriveSystemIcon('.xlsx');
    expect(calls[0].name).toBe('file.xlsx');
  });

  it('remembers a type the host has no icon for instead of retrying per row', async () => {
    const calls = installHostIcons(() => '');

    expect(await loadCloudDriveSystemIcon('.wav')).toEqual({ state: 'absent' });
    expect(await loadCloudDriveSystemIcon('.wav')).toEqual({ state: 'absent' });
    expect(calls).toHaveLength(1);
    expect(peekCloudDriveSystemIcon('.wav')).toBeNull();
  });

  it('retries a transient host failure instead of dropping the type for the session', async () => {
    let fail = true;
    const calls = installHostIcons(() => {
      if (fail) throw new Error('shell busy');
      return 'data:image/png;base64,RECOVERED';
    });

    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual({ state: 'failed' });
    expect(calls).toHaveLength(1);
    expect(peekCloudDriveSystemIcon('.pdf')).toBeNull();

    // The next row asks again: a momentary fault must not cost the type its icon.
    fail = false;
    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual({
      state: 'icon',
      url: 'data:image/png;base64,RECOVERED',
    });
    expect(calls).toHaveLength(2);
  });

  it('gives up on a host that keeps failing so rows do not ask forever', async () => {
    const calls = installHostIcons(() => { throw new Error('no icon mechanism'); });

    for (let attempt = 0; attempt < 3; attempt += 1) {
      expect(await loadCloudDriveSystemIcon('.tex')).toEqual({ state: 'failed' });
    }
    expect(calls).toHaveLength(3);

    // The budget is spent: this suffix is treated as absent from now on.
    expect(await loadCloudDriveSystemIcon('.tex')).toEqual({ state: 'absent' });
    expect(calls).toHaveLength(3);
  });

  it('stops serving an icon the WebView rejected, and stops asking for it', async () => {
    const calls = installHostIcons(() => 'data:image/png;base64,CORRUPT');
    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual({
      state: 'icon',
      url: 'data:image/png;base64,CORRUPT',
    });

    forgetCloudDriveSystemIcon('.pdf');

    // The corrupt answer is no longer served, and the type is not asked again:
    // otherwise every mount would repaint a broken image.
    expect(peekCloudDriveSystemIcon('.pdf')).toBeNull();
    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual({ state: 'absent' });
    expect(calls).toHaveLength(1);
  });

  it('returns no icon without asking when the suffix is unknown', async () => {
    const calls = installHostIcons(() => 'data:image/png;base64,X');
    expect(await loadCloudDriveSystemIcon('')).toEqual({ state: 'absent' });
    expect(calls).toHaveLength(0);
  });

  it('treats a missing desktop binding as absent rather than a retryable fault', async () => {
    (window as unknown as { go: unknown }).go = { main: { App: {} } };
    expect(await loadCloudDriveSystemIcon('.pdf')).toEqual({ state: 'absent' });
  });
});
