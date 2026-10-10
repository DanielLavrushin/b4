import { KeyboardEvent, PointerEvent, useEffect, useRef, useState } from "react";
import { Box, IconButton, Stack, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, typography } from "@design";
import { GameIcon } from "@b4.icons";

const COLS = 24;
const ROWS = 14;
const CELL = 20;
const WIDTH = COLS * CELL;
const HEIGHT = ROWS * CELL;
const START_STEP_MS = 150;
const MIN_STEP_MS = 60;
const SPEEDUP_MS = 4;
const SWIPE_PX = 24;
const BEST_KEY = "b4_snake_best";
const FOUND_KEY = "b4_snake_found";

type Dir = "up" | "down" | "left" | "right";
type Status = "ready" | "playing" | "paused" | "over";

interface Point {
  x: number;
  y: number;
}

interface Game {
  snake: Point[];
  dir: Dir;
  queue: Dir[];
  food: Point | null;
  stepMs: number;
  score: number;
}

const DIRS: Record<Dir, Point> = {
  up: { x: 0, y: -1 },
  down: { x: 0, y: 1 },
  left: { x: -1, y: 0 },
  right: { x: 1, y: 0 },
};

const KEYS: Record<string, Dir> = {
  ArrowUp: "up",
  KeyW: "up",
  ArrowDown: "down",
  KeyS: "down",
  ArrowLeft: "left",
  KeyA: "left",
  ArrowRight: "right",
  KeyD: "right",
};

const same = (a: Point, b: Point) => a.x === b.x && a.y === b.y;

const opposite = (a: Dir, b: Dir) =>
  DIRS[a].x + DIRS[b].x === 0 && DIRS[a].y + DIRS[b].y === 0;

const placeFood = (snake: Point[]): Point | null => {
  const free: Point[] = [];
  for (let y = 0; y < ROWS; y++) {
    for (let x = 0; x < COLS; x++) {
      if (!snake.some((p) => p.x === x && p.y === y)) free.push({ x, y });
    }
  }
  return free.length > 0
    ? free[Math.floor(Math.random() * free.length)]
    : null;
};

const newGame = (): Game => {
  const y = Math.floor(ROWS / 2);
  const snake = [
    { x: 6, y },
    { x: 5, y },
    { x: 4, y },
  ];
  return {
    snake,
    dir: "right",
    queue: [],
    food: placeFood(snake),
    stepMs: START_STEP_MS,
    score: 0,
  };
};

const turn = (g: Game, dir: Dir) => {
  const last = g.queue.at(-1) ?? g.dir;
  if (dir === last || opposite(dir, last) || g.queue.length >= 2) return;
  g.queue.push(dir);
};

const advance = (g: Game): "moved" | "ate" | "dead" => {
  const next = g.queue.shift();
  if (next) g.dir = next;
  const head = {
    x: g.snake[0].x + DIRS[g.dir].x,
    y: g.snake[0].y + DIRS[g.dir].y,
  };
  const ate = !!g.food && same(head, g.food);
  const body = ate ? g.snake : g.snake.slice(0, -1);
  if (
    head.x < 0 ||
    head.y < 0 ||
    head.x >= COLS ||
    head.y >= ROWS ||
    body.some((p) => same(p, head))
  ) {
    return "dead";
  }
  g.snake = [head, ...body];
  if (!ate) return "moved";
  g.score += 1;
  g.stepMs = Math.max(MIN_STEP_MS, g.stepMs - SPEEDUP_MS);
  g.food = placeFood(g.snake);
  return g.food ? "ate" : "dead";
};

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

const HEAD = rgb(colors.secondary);
const TAIL = rgb(colors.primary);

const segmentColor = (i: number, length: number) => {
  const k = length > 1 ? i / (length - 1) : 0;
  const [r, g, b] = HEAD.map((c, j) => Math.round(c + (TAIL[j] - c) * k));
  return `rgb(${r}, ${g}, ${b})`;
};

const draw = (canvas: HTMLCanvasElement | null, g: Game) => {
  const ctx = canvas?.getContext("2d");
  if (!canvas || !ctx) return;
  const width = Math.round(WIDTH * (window.devicePixelRatio || 1));
  if (canvas.width !== width) {
    canvas.width = width;
    canvas.height = Math.round((width * HEIGHT) / WIDTH);
  }
  const scale = width / WIDTH;
  ctx.setTransform(scale, 0, 0, scale, 0, 0);
  ctx.fillStyle = colors.background.dark;
  ctx.fillRect(0, 0, WIDTH, HEIGHT);

  ctx.fillStyle = colors.border.light;
  for (let y = 0; y < ROWS; y++) {
    for (let x = 0; x < COLS; x++) {
      ctx.fillRect(x * CELL + CELL / 2 - 1, y * CELL + CELL / 2 - 1, 2, 2);
    }
  }

  if (g.food) {
    ctx.save();
    ctx.fillStyle = colors.primaryLight;
    ctx.shadowColor = colors.primaryLight;
    ctx.shadowBlur = 12;
    ctx.fillRect(g.food.x * CELL + 4, g.food.y * CELL + 4, CELL - 8, CELL - 8);
    ctx.restore();
  }

  g.snake.forEach((p, i) => {
    ctx.fillStyle = segmentColor(i, g.snake.length);
    ctx.fillRect(p.x * CELL + 1, p.y * CELL + 1, CELL - 2, CELL - 2);
  });

  const head = g.snake[0];
  const d = DIRS[g.dir];
  const cx = head.x * CELL + CELL / 2 + d.x * 4;
  const cy = head.y * CELL + CELL / 2 + d.y * 4;
  ctx.fillStyle = colors.background.dark;
  ctx.fillRect(cx - 1.5 + d.y * 4, cy - 1.5 + d.x * 4, 3, 3);
  ctx.fillRect(cx - 1.5 - d.y * 4, cy - 1.5 - d.x * 4, 3, 3);
};

const loadBest = () => {
  try {
    return Number(localStorage.getItem(BEST_KEY)) || 0;
  } catch {
    return 0;
  }
};

const saveBest = (best: number) => {
  try {
    localStorage.setItem(BEST_KEY, String(best));
  } catch {
    return;
  }
};

const loadFound = () => {
  try {
    return localStorage.getItem(FOUND_KEY) === "1";
  } catch {
    return false;
  }
};

const saveFound = () => {
  try {
    localStorage.setItem(FOUND_KEY, "1");
  } catch {
    return;
  }
};

interface SnakeButtonProps {
  open: boolean;
  nudge: boolean;
  onToggle: () => void;
}

export const SnakeButton = ({ open, nudge, onToggle }: SnakeButtonProps) => {
  const { t } = useTranslation();
  const [found, setFound] = useState(loadFound);

  const toggle = () => {
    if (!found) {
      saveFound();
      setFound(true);
    }
    onToggle();
  };

  return (
    <Tooltip title={open ? t("discovery.snake.hide") : t("discovery.snake.show")}>
      <IconButton
        size="small"
        onClick={toggle}
        sx={{
          color: open ? colors.secondary : colors.text.disabled,
          "&:hover": { color: colors.secondary },
          "@keyframes b4SnakeNudge": {
            "0%, 14%, 100%": { transform: "rotate(0deg) scale(1)" },
            "2%": { transform: "rotate(-18deg) scale(1.2)" },
            "4%": { transform: "rotate(16deg) scale(1.2)" },
            "6%": { transform: "rotate(-12deg) scale(1.15)" },
            "8%": { transform: "rotate(10deg) scale(1.1)" },
            "10%": { transform: "rotate(-5deg) scale(1.05)" },
            "12%": { transform: "rotate(2deg) scale(1)" },
          },
          ...(nudge && !found
            ? { animation: "b4SnakeNudge 6s ease-in-out infinite" }
            : {}),
          "@media (prefers-reduced-motion: reduce)": { animation: "none" },
        }}
      >
        <GameIcon fontSize="small" />
      </IconButton>
    </Tooltip>
  );
};

export const Snake = () => {
  const { t } = useTranslation();
  const boardRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const game = useRef<Game>(newGame());
  const swipe = useRef<Point | null>(null);
  const [status, setStatus] = useState<Status>("ready");
  const [score, setScore] = useState(0);
  const [best, setBest] = useState(loadBest);

  useEffect(() => {
    draw(canvasRef.current, game.current);
    boardRef.current?.focus({ preventScroll: true });
    boardRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, []);

  useEffect(() => {
    if (status !== "playing") return;
    let raf = 0;
    let last = performance.now();
    let pending = 0;
    const frame = (now: number) => {
      pending += Math.min(now - last, 250);
      last = now;
      while (pending >= game.current.stepMs) {
        pending -= game.current.stepMs;
        const outcome = advance(game.current);
        if (outcome !== "moved") {
          const points = game.current.score;
          setScore(points);
          setBest((prev) => {
            if (points <= prev) return prev;
            saveBest(points);
            return points;
          });
        }
        if (outcome === "dead") {
          draw(canvasRef.current, game.current);
          setStatus("over");
          return;
        }
      }
      draw(canvasRef.current, game.current);
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [status]);

  const play = (dir?: Dir) => {
    if (status === "ready" || status === "over") {
      game.current = newGame();
      setScore(0);
      draw(canvasRef.current, game.current);
    }
    if (dir) turn(game.current, dir);
    setStatus("playing");
  };

  const toggle = () => {
    if (status === "playing") setStatus("paused");
    else play();
  };

  const steer = (dir: Dir) => {
    if (status === "playing") turn(game.current, dir);
    else if (status !== "over") play(dir);
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const dir = KEYS[e.code];
    if (dir) {
      e.preventDefault();
      steer(dir);
    } else if (e.code === "Space" || e.code === "Enter" || e.code === "KeyP") {
      e.preventDefault();
      toggle();
    } else if (e.code === "Escape" && status === "playing") {
      setStatus("paused");
    }
  };

  const onPointerDown = (e: PointerEvent) => {
    swipe.current = { x: e.clientX, y: e.clientY };
  };

  const onPointerUp = (e: PointerEvent) => {
    const from = swipe.current;
    swipe.current = null;
    if (!from) return;
    const dx = e.clientX - from.x;
    const dy = e.clientY - from.y;
    if (Math.max(Math.abs(dx), Math.abs(dy)) < SWIPE_PX) {
      toggle();
      return;
    }
    if (Math.abs(dx) > Math.abs(dy)) steer(dx > 0 ? "right" : "left");
    else steer(dy > 0 ? "down" : "up");
  };

  const overlay =
    status === "ready"
      ? { title: t("discovery.snake.title"), hint: t("discovery.snake.start") }
      : status === "paused"
        ? { title: t("discovery.snake.paused"), hint: t("discovery.snake.resume") }
        : status === "over"
          ? {
              title: t("discovery.snake.over", { score }),
              hint: t("discovery.snake.restart"),
            }
          : null;

  return (
    <Stack spacing={1} sx={{ alignItems: "center" }}>
      <Stack
        direction="row"
        justifyContent="space-between"
        sx={{
          width: "100%",
          maxWidth: WIDTH,
          ...typography.recipes.monoSmall,
          color: colors.text.secondary,
        }}
      >
        <span>{t("discovery.snake.score", { score })}</span>
        <span>{t("discovery.snake.best", { best })}</span>
      </Stack>
      <Box
        ref={boardRef}
        tabIndex={0}
        role="application"
        aria-label={t("discovery.snake.title")}
        onKeyDown={onKeyDown}
        onBlur={() => {
          if (status === "playing") setStatus("paused");
        }}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={() => {
          swipe.current = null;
        }}
        sx={{
          position: "relative",
          width: "100%",
          maxWidth: WIDTH,
          aspectRatio: `${COLS} / ${ROWS}`,
          border: `1px solid ${colors.border.default}`,
          borderRadius: 1.5,
          overflow: "hidden",
          touchAction: "none",
          userSelect: "none",
          cursor: "pointer",
          outline: "none",
          "&:focus-visible": { borderColor: colors.secondary },
        }}
      >
        <canvas
          ref={canvasRef}
          style={{ display: "block", width: "100%", height: "100%" }}
        />
        {overlay && (
          <Box
            sx={{
              position: "absolute",
              inset: 0,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              gap: 1,
              px: 2,
              textAlign: "center",
              bgcolor: "rgba(15, 10, 14, 0.72)",
            }}
          >
            <Typography sx={{ fontWeight: 600, color: colors.secondary }}>
              {overlay.title}
            </Typography>
            <Typography
              variant="caption"
              sx={{ color: colors.text.secondary }}
            >
              {overlay.hint}
            </Typography>
          </Box>
        )}
      </Box>
      <Typography
        variant="caption"
        sx={{ ...typography.recipes.monoSmall, color: colors.text.disabled }}
      >
        {t("discovery.snake.help")}
      </Typography>
    </Stack>
  );
};
