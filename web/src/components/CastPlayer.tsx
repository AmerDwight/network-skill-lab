import { create } from "asciinema-player";
import { useEffect, useRef } from "react";

import "asciinema-player/dist/bundle/asciinema-player.css";

export const idleTimeLimit = 2;

export default function CastPlayer({
  src,
  speed,
}: {
  src: string;
  speed: number;
}) {
  const container = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = container.current;
    if (element === null) {
      return;
    }
    const player = create(src, element, {
      speed,
      idleTimeLimit,
      fit: "width",
    });
    return () => {
      player.dispose();
    };
  }, [src, speed]);

  return <div className="cast-player" ref={container} />;
}
