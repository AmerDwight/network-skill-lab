import { cleanup, render, waitFor } from "@testing-library/react";
import type { Player } from "asciinema-player";
import { create } from "asciinema-player";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CastPlayer, { idleTimeLimit } from "./CastPlayer";

vi.mock("asciinema-player", () => ({ create: vi.fn() }));

const createPlayer = vi.mocked(create);
const dispose = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  createPlayer.mockImplementation(() => ({ dispose }) as unknown as Player);
});

afterEach(() => {
  cleanup();
});

describe("CastPlayer", () => {
  it("mounts the player on the cast url with the playback options", async () => {
    const { container } = render(<CastPlayer src="/cast" speed={1} />);

    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(1);
    });
    expect(createPlayer).toHaveBeenCalledWith(
      "/cast",
      container.querySelector(".cast-player"),
      { speed: 1, idleTimeLimit, fit: "width" },
    );
    expect(idleTimeLimit).toBe(2);
  });

  it("recreates the player when the speed changes", async () => {
    const { rerender } = render(<CastPlayer src="/cast" speed={1} />);
    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(1);
    });

    rerender(<CastPlayer src="/cast" speed={4} />);

    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(2);
    });
    expect(dispose).toHaveBeenCalledTimes(1);
    expect(createPlayer.mock.calls[1][2]?.speed).toBe(4);
  });

  it("disposes the player when it goes away", async () => {
    const { unmount } = render(<CastPlayer src="/cast" speed={1} />);
    await waitFor(() => {
      expect(createPlayer).toHaveBeenCalledTimes(1);
    });

    unmount();

    expect(dispose).toHaveBeenCalledTimes(1);
  });
});
