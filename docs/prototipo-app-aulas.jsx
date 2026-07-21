import React, { useState } from "react";

/* ─────────────────────────────────────────────────────────────
   Protótipo navegável — app desktop de arquivo de aulas (Cambly)
   Telas: Biblioteca · Detalhe da aula · Progresso · Fila
   Modelo de sync pull-work-push refletido no cabeçalho
   ───────────────────────────────────────────────────────────── */

const C = {
  bg: "#14181F",
  surface: "#1B222B",
  surface2: "#222B36",
  line: "#2B3542",
  text: "#E9EDF2",
  mut: "#8B99AB",
  blue: "#6EA8FE",
  amber: "#E3A44C",
  green: "#6FBF8E",
  red: "#E06C6C",
};

const F = {
  display: "'Sora', system-ui, sans-serif",
  body: "'Inter', system-ui, sans-serif",
  mono: "'JetBrains Mono', ui-monospace, monospace",
};

const LESSONS = [
  {
    id: 1,
    date: "15 jul 2026",
    tutor: "Sarah M.",
    duration: "31 min",
    topics: ["Viagem", "Small talk"],
    words: 8,
    errors: 3,
    status: "done",
  },
  {
    id: 2,
    date: "11 jul 2026",
    tutor: "James K.",
    duration: "29 min",
    topics: ["Trabalho", "Entrevistas"],
    words: 11,
    errors: 5,
    status: "done",
  },
  {
    id: 3,
    date: "08 jul 2026",
    tutor: "Sarah M.",
    duration: "33 min",
    topics: ["Filmes", "Opiniões"],
    words: null,
    errors: null,
    status: "processing",
  },
  {
    id: 4,
    date: "04 jul 2026",
    tutor: "Priya R.",
    duration: "30 min",
    topics: ["Tecnologia", "Trabalho"],
    words: 6,
    errors: 4,
    status: "done",
  },
  {
    id: 5,
    date: "30 jun 2026",
    tutor: "James K.",
    duration: "28 min",
    topics: ["Rotina", "Small talk"],
    words: 9,
    errors: 6,
    status: "done",
  },
];

/* Transcrição da aula 1 — partes com correção inline:
   {wrong, right} = erro do aluno riscado + correção do lado */
const TRANSCRIPT = [
  {
    t: "00:12",
    s: 12,
    who: "tutor",
    parts: [{ text: "So, tell me about your last trip. Where did you go?" }],
  },
  {
    t: "00:18",
    s: 18,
    who: "me",
    parts: [
      { text: "Last year I " },
      { wrong: "have traveled", right: "traveled" },
      { text: " to Chile with my family. It was my first time there." },
    ],
  },
  {
    t: "00:29",
    s: 29,
    who: "tutor",
    parts: [
      {
        text: "That sounds amazing! Chile is beautiful. Did you visit the Atacama Desert?",
      },
    ],
  },
  {
    t: "00:37",
    s: 37,
    who: "me",
    parts: [
      { text: "Yes! The landscape was breathtaking. We " },
      { wrong: "stayed during", right: "stayed for" },
      { text: " four days in San Pedro." },
    ],
  },
  {
    t: "00:49",
    s: 49,
    who: "tutor",
    parts: [
      {
        text: "Four days is a good amount of time. It's a bit off the beaten path, isn't it?",
      },
    ],
  },
  {
    t: "00:56",
    s: 56,
    who: "me",
    parts: [
      { text: "Sorry, what " },
      { wrong: "means", right: "does" },
      { text: " \"off the beaten path\" " },
      { wrong: "", right: "mean" },
      { text: "?" },
    ],
  },
  {
    t: "01:02",
    s: 62,
    who: "tutor",
    parts: [
      {
        text: "It means a place that is remote, that most tourists don't visit. Great question!",
      },
    ],
  },
];

const ANALYSIS = {
  vocab: [
    ["layover", "escala (de voo)"],
    ["breathtaking", "de tirar o fôlego"],
    ["off the beaten path", "fora do circuito turístico"],
    ["altitude sickness", "mal de altitude"],
  ],
  tutorPhrases: [
    "That sounds amazing!",
    "a good amount of time",
    "Great question!",
  ],
  recurring: [
    ["Present perfect × simple past", "3ª ocorrência este mês"],
    ["Preposições com verbos (stay for)", "2ª ocorrência"],
  ],
};

const QUEUE = [
  {
    file: "cambly-2026-07-08.mp4",
    step: "Transcrevendo áudio",
    pct: 64,
    state: "run",
  },
  {
    file: "cambly-2026-07-04.mp4",
    step: "Análise concluída",
    pct: 100,
    state: "done",
  },
  {
    file: "cambly-2026-06-30.mp4",
    step: "Análise concluída",
    pct: 100,
    state: "done",
  },
];

const PROGRESS_ERRORS = [
  { name: "Present perfect × simple past", count: 9, trend: "↘ melhorando" },
  { name: "Preposições (in/on/at, for/during)", count: 7, trend: "→ estável" },
  { name: "Ordem em perguntas indiretas", count: 4, trend: "↘ melhorando" },
  { name: "Plural de substantivos irregulares", count: 2, trend: "✓ superado" },
];

const VOCAB_WEEKS = [
  ["Sem 1", 6],
  ["Sem 2", 9],
  ["Sem 3", 11],
  ["Sem 4", 8],
];

/* ── átomos ── */

const Tag = ({ children }) => (
  <span
    className="px-2 py-0.5 rounded-full text-xs"
    style={{
      background: C.surface2,
      color: C.mut,
      border: `1px solid ${C.line}`,
      fontFamily: F.body,
    }}
  >
    {children}
  </span>
);

const Dot = ({ color }) => (
  <span
    className="inline-block w-2 h-2 rounded-full mr-2"
    style={{ background: color }}
  />
);

function SyncPill({ pending, onPush }) {
  return (
    <button
      onClick={pending ? onPush : undefined}
      className="flex items-center px-3 py-1.5 rounded-full text-xs transition-opacity hover:opacity-80"
      style={{
        background: C.surface2,
        border: `1px solid ${pending ? C.amber : C.line}`,
        color: pending ? C.amber : C.mut,
        fontFamily: F.body,
        cursor: pending ? "pointer" : "default",
      }}
      title={
        pending
          ? "Enviar alterações locais para a pasta do Google Drive"
          : "Banco local em dia com a cópia do Drive"
      }
    >
      <Dot color={pending ? C.amber : C.green} />
      {pending ? "2 alterações pendentes — enviar ao Drive" : "Sincronizado com o Drive"}
    </button>
  );
}

/* ── telas ── */

function Library({ open, onImport, query, setQuery }) {
  const filtered = LESSONS.filter(
    (l) =>
      l.tutor.toLowerCase().includes(query.toLowerCase()) ||
      l.topics.some((t) => t.toLowerCase().includes(query.toLowerCase())) ||
      query === ""
  );
  return (
    <div className="p-8 max-w-4xl mx-auto w-full">
      <div className="flex items-end justify-between mb-6">
        <div>
          <h1
            className="text-2xl mb-1"
            style={{ fontFamily: F.display, color: C.text, fontWeight: 600 }}
          >
            Biblioteca
          </h1>
          <p className="text-sm" style={{ color: C.mut, fontFamily: F.body }}>
            {LESSONS.length} aulas arquivadas · 14h 32min de conversação
          </p>
        </div>
        <button
          onClick={onImport}
          className="px-4 py-2 rounded-lg text-sm hover:opacity-90"
          style={{
            background: C.blue,
            color: "#0D1420",
            fontFamily: F.body,
            fontWeight: 600,
          }}
        >
          + Importar aula
        </button>
      </div>

      <input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Buscar por tutor, tópico ou trecho da conversa…"
        className="w-full mb-6 px-4 py-2.5 rounded-lg text-sm outline-none"
        style={{
          background: C.surface,
          border: `1px solid ${C.line}`,
          color: C.text,
          fontFamily: F.body,
        }}
      />

      <div className="flex flex-col gap-3">
        {filtered.map((l) => (
          <button
            key={l.id}
            onClick={() => l.status === "done" && open(l)}
            className="text-left rounded-xl p-4 flex items-center gap-4 transition-colors"
            style={{
              background: C.surface,
              border: `1px solid ${C.line}`,
              cursor: l.status === "done" ? "pointer" : "default",
              opacity: l.status === "done" ? 1 : 0.7,
            }}
          >
            <div
              className="w-20 h-12 rounded-lg flex items-center justify-center shrink-0"
              style={{ background: C.surface2, border: `1px solid ${C.line}` }}
            >
              <span style={{ color: C.mut, fontSize: 18 }}>▶</span>
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-3 mb-1.5 flex-wrap">
                <span
                  className="text-sm"
                  style={{ color: C.text, fontFamily: F.body, fontWeight: 600 }}
                >
                  {l.date}
                </span>
                <span className="text-xs" style={{ color: C.mut, fontFamily: F.mono }}>
                  {l.tutor} · {l.duration}
                </span>
              </div>
              <div className="flex gap-1.5 flex-wrap">
                {l.topics.map((t) => (
                  <Tag key={t}>{t}</Tag>
                ))}
              </div>
            </div>
            <div className="text-right shrink-0">
              {l.status === "done" ? (
                <div className="text-xs" style={{ color: C.mut, fontFamily: F.body }}>
                  <div style={{ color: C.green }}>{l.words} palavras novas</div>
                  <div style={{ color: C.amber }}>{l.errors} correções</div>
                </div>
              ) : (
                <span
                  className="text-xs px-2 py-1 rounded-full"
                  style={{
                    color: C.blue,
                    background: "rgba(110,168,254,.1)",
                    fontFamily: F.body,
                  }}
                >
                  processando…
                </span>
              )}
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}

function LessonDetail({ lesson, back }) {
  const [line, setLine] = useState(1);
  const [tab, setTab] = useState("transcript");
  const cur = TRANSCRIPT[line];

  return (
    <div className="p-8 max-w-5xl mx-auto w-full">
      <button
        onClick={back}
        className="text-sm mb-4 hover:opacity-80"
        style={{ color: C.mut, fontFamily: F.body }}
      >
        ← Biblioteca
      </button>

      <div className="flex items-center gap-3 mb-5 flex-wrap">
        <h1
          className="text-xl"
          style={{ fontFamily: F.display, color: C.text, fontWeight: 600 }}
        >
          Aula de {lesson.date}
        </h1>
        <span className="text-sm" style={{ color: C.mut, fontFamily: F.mono }}>
          {lesson.tutor} · {lesson.duration}
        </span>
        {lesson.topics.map((t) => (
          <Tag key={t}>{t}</Tag>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        {/* player simulado */}
        <div>
          <div
            className="rounded-xl aspect-video flex flex-col items-center justify-center relative overflow-hidden"
            style={{ background: "#0B0E13", border: `1px solid ${C.line}` }}
          >
            <div
              className="w-14 h-14 rounded-full flex items-center justify-center mb-3"
              style={{ background: "rgba(110,168,254,.15)" }}
            >
              <span style={{ color: C.blue, fontSize: 22 }}>▶</span>
            </div>
            <span className="text-xs" style={{ color: C.mut, fontFamily: F.mono }}>
              {cur.t} / 31:04 — sincronizado com a transcrição
            </span>
            <div
              className="absolute bottom-0 left-0 h-1"
              style={{
                background: C.blue,
                width: `${(cur.s / 1864) * 100 + 3}%`,
              }}
            />
          </div>
          <p
            className="text-xs mt-3 leading-relaxed"
            style={{ color: C.mut, fontFamily: F.body }}
          >
            Clique em qualquer fala ao lado para pular o vídeo até aquele momento.
            Suas falas aparecem em azul; correções sugeridas pela análise aparecem
            em <span style={{ color: C.amber }}>âmbar</span>, com o original riscado.
          </p>
        </div>

        {/* painel transcrição / análise */}
        <div
          className="rounded-xl overflow-hidden"
          style={{ background: C.surface, border: `1px solid ${C.line}` }}
        >
          <div className="flex" style={{ borderBottom: `1px solid ${C.line}` }}>
            {[
              ["transcript", "Transcrição"],
              ["analysis", "Análise"],
            ].map(([k, label]) => (
              <button
                key={k}
                onClick={() => setTab(k)}
                className="px-4 py-3 text-sm"
                style={{
                  color: tab === k ? C.text : C.mut,
                  fontFamily: F.body,
                  fontWeight: tab === k ? 600 : 400,
                  borderBottom:
                    tab === k ? `2px solid ${C.blue}` : "2px solid transparent",
                }}
              >
                {label}
              </button>
            ))}
          </div>

          {tab === "transcript" ? (
            <div className="p-4 flex flex-col gap-1 overflow-y-auto" style={{ maxHeight: 420 }}>
              {TRANSCRIPT.map((seg, i) => (
                <button
                  key={i}
                  onClick={() => setLine(i)}
                  className="text-left rounded-lg px-3 py-2.5 transition-colors"
                  style={{
                    background: i === line ? C.surface2 : "transparent",
                    borderLeft: `3px solid ${
                      seg.who === "me" ? C.blue : "transparent"
                    }`,
                  }}
                >
                  <div className="flex gap-2 items-baseline mb-0.5">
                    <span
                      className="text-xs"
                      style={{ color: C.mut, fontFamily: F.mono }}
                    >
                      {seg.t}
                    </span>
                    <span
                      className="text-xs uppercase tracking-wide"
                      style={{
                        color: seg.who === "me" ? C.blue : C.green,
                        fontFamily: F.body,
                        fontWeight: 600,
                      }}
                    >
                      {seg.who === "me" ? "Você" : "Tutor"}
                    </span>
                  </div>
                  <p
                    className="text-sm leading-relaxed"
                    style={{ color: C.text, fontFamily: F.body }}
                  >
                    {seg.parts.map((p, j) =>
                      p.text !== undefined && p.wrong === undefined ? (
                        <span key={j}>{p.text}</span>
                      ) : (
                        <span key={j}>
                          {p.wrong && (
                            <span
                              style={{
                                textDecoration: "line-through",
                                color: C.mut,
                              }}
                            >
                              {p.wrong}
                            </span>
                          )}{" "}
                          <span style={{ color: C.amber, fontWeight: 600 }}>
                            {p.right}
                          </span>
                        </span>
                      )
                    )}
                  </p>
                </button>
              ))}
            </div>
          ) : (
            <div className="p-5 flex flex-col gap-5 overflow-y-auto" style={{ maxHeight: 420 }}>
              <section>
                <h3
                  className="text-xs uppercase tracking-widest mb-2"
                  style={{ color: C.green, fontFamily: F.body, fontWeight: 700 }}
                >
                  Vocabulário novo
                </h3>
                {ANALYSIS.vocab.map(([en, pt]) => (
                  <div key={en} className="flex justify-between py-1.5 text-sm" style={{ borderBottom: `1px solid ${C.line}` }}>
                    <span style={{ color: C.text, fontFamily: F.body, fontWeight: 600 }}>{en}</span>
                    <span style={{ color: C.mut, fontFamily: F.body }}>{pt}</span>
                  </div>
                ))}
              </section>
              <section>
                <h3
                  className="text-xs uppercase tracking-widest mb-2"
                  style={{ color: C.blue, fontFamily: F.body, fontWeight: 700 }}
                >
                  Expressões do tutor para reutilizar
                </h3>
                <div className="flex gap-2 flex-wrap">
                  {ANALYSIS.tutorPhrases.map((p) => (
                    <Tag key={p}>{p}</Tag>
                  ))}
                </div>
              </section>
              <section>
                <h3
                  className="text-xs uppercase tracking-widest mb-2"
                  style={{ color: C.amber, fontFamily: F.body, fontWeight: 700 }}
                >
                  Erros recorrentes nesta aula
                </h3>
                {ANALYSIS.recurring.map(([name, note]) => (
                  <div key={name} className="py-1.5 text-sm">
                    <span style={{ color: C.text, fontFamily: F.body }}>{name}</span>
                    <span className="ml-2 text-xs" style={{ color: C.mut }}>{note}</span>
                  </div>
                ))}
              </section>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function Progress() {
  const max = Math.max(...VOCAB_WEEKS.map(([, v]) => v));
  return (
    <div className="p-8 max-w-4xl mx-auto w-full">
      <h1
        className="text-2xl mb-1"
        style={{ fontFamily: F.display, color: C.text, fontWeight: 600 }}
      >
        Progresso
      </h1>
      <p className="text-sm mb-7" style={{ color: C.mut, fontFamily: F.body }}>
        Visão agregada das últimas 12 aulas — julho de 2026
      </p>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        <div
          className="rounded-xl p-5"
          style={{ background: C.surface, border: `1px solid ${C.line}` }}
        >
          <h3
            className="text-xs uppercase tracking-widest mb-4"
            style={{ color: C.amber, fontFamily: F.body, fontWeight: 700 }}
          >
            Erros recorrentes
          </h3>
          {PROGRESS_ERRORS.map((e) => (
            <div key={e.name} className="mb-3">
              <div className="flex justify-between text-sm mb-1">
                <span style={{ color: C.text, fontFamily: F.body }}>{e.name}</span>
                <span
                  className="text-xs"
                  style={{
                    color: e.trend.includes("✓") ? C.green : C.mut,
                    fontFamily: F.body,
                  }}
                >
                  {e.trend}
                </span>
              </div>
              <div className="h-1.5 rounded-full" style={{ background: C.surface2 }}>
                <div
                  className="h-1.5 rounded-full"
                  style={{
                    background: e.trend.includes("✓") ? C.green : C.amber,
                    width: `${(e.count / 9) * 100}%`,
                  }}
                />
              </div>
            </div>
          ))}
        </div>

        <div
          className="rounded-xl p-5"
          style={{ background: C.surface, border: `1px solid ${C.line}` }}
        >
          <h3
            className="text-xs uppercase tracking-widest mb-4"
            style={{ color: C.green, fontFamily: F.body, fontWeight: 700 }}
          >
            Vocabulário novo por semana
          </h3>
          <div className="flex items-end gap-4 h-40 mb-2">
            {VOCAB_WEEKS.map(([w, v]) => (
              <div key={w} className="flex-1 flex flex-col items-center gap-1.5">
                <span className="text-xs" style={{ color: C.text, fontFamily: F.mono }}>
                  {v}
                </span>
                <div
                  className="w-full rounded-t-md"
                  style={{
                    background: C.green,
                    opacity: 0.85,
                    height: `${(v / max) * 100}%`,
                  }}
                />
                <span className="text-xs" style={{ color: C.mut, fontFamily: F.body }}>
                  {w}
                </span>
              </div>
            ))}
          </div>
          <p className="text-xs" style={{ color: C.mut, fontFamily: F.body }}>
            34 palavras e expressões registradas no mês · 214 acumuladas
          </p>
        </div>
      </div>

      <div
        className="rounded-xl p-5 mt-5"
        style={{ background: C.surface, border: `1px solid ${C.line}` }}
      >
        <h3
          className="text-xs uppercase tracking-widest mb-2"
          style={{ color: C.blue, fontFamily: F.body, fontWeight: 700 }}
        >
          Caderno de revisão sugerido
        </h3>
        <p className="text-sm leading-relaxed" style={{ color: C.text, fontFamily: F.body }}>
          Antes da próxima aula, revise: <b>present perfect × simple past</b> (apareceu
          em 3 das últimas 4 aulas) e as preposições <b>for / during</b>. Tente usar as
          expressões <i>off the beaten path</i> e <i>breathtaking</i> na próxima conversa.
        </p>
      </div>
    </div>
  );
}

function Queue() {
  return (
    <div className="p-8 max-w-3xl mx-auto w-full">
      <h1
        className="text-2xl mb-1"
        style={{ fontFamily: F.display, color: C.text, fontWeight: 600 }}
      >
        Fila de processamento
      </h1>
      <p className="text-sm mb-7" style={{ color: C.mut, fontFamily: F.body }}>
        Transcrição e análise rodam em segundo plano — você pode fechar o app.
      </p>
      <div className="flex flex-col gap-3">
        {QUEUE.map((j) => (
          <div
            key={j.file}
            className="rounded-xl p-4"
            style={{ background: C.surface, border: `1px solid ${C.line}` }}
          >
            <div className="flex justify-between items-center mb-2">
              <span className="text-sm" style={{ color: C.text, fontFamily: F.mono }}>
                {j.file}
              </span>
              <span
                className="text-xs"
                style={{
                  color: j.state === "done" ? C.green : C.blue,
                  fontFamily: F.body,
                }}
              >
                {j.step}
              </span>
            </div>
            <div className="h-1.5 rounded-full" style={{ background: C.surface2 }}>
              <div
                className="h-1.5 rounded-full transition-all"
                style={{
                  background: j.state === "done" ? C.green : C.blue,
                  width: `${j.pct}%`,
                }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function ImportModal({ close }) {
  return (
    <div
      className="fixed inset-0 flex items-center justify-center z-50 p-6"
      style={{ background: "rgba(0,0,0,.6)" }}
      onClick={close}
    >
      <div
        className="rounded-2xl p-6 w-full max-w-md"
        style={{ background: C.surface, border: `1px solid ${C.line}` }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2
          className="text-lg mb-4"
          style={{ fontFamily: F.display, color: C.text, fontWeight: 600 }}
        >
          Importar aula
        </h2>
        <div
          className="rounded-xl flex flex-col items-center justify-center py-10 mb-4 text-center"
          style={{ border: `2px dashed ${C.line}`, color: C.mut, fontFamily: F.body }}
        >
          <span className="text-2xl mb-2">⬇</span>
          <p className="text-sm">Arraste o vídeo da aula aqui</p>
          <p className="text-xs mt-1">ou clique para escolher o arquivo</p>
        </div>
        <div className="grid grid-cols-2 gap-3 mb-5">
          <div>
            <label className="text-xs block mb-1" style={{ color: C.mut, fontFamily: F.body }}>
              Data da aula
            </label>
            <div
              className="px-3 py-2 rounded-lg text-sm"
              style={{ background: C.surface2, border: `1px solid ${C.line}`, color: C.text, fontFamily: F.body }}
            >
              17/07/2026
            </div>
          </div>
          <div>
            <label className="text-xs block mb-1" style={{ color: C.mut, fontFamily: F.body }}>
              Tutor
            </label>
            <div
              className="px-3 py-2 rounded-lg text-sm"
              style={{ background: C.surface2, border: `1px solid ${C.line}`, color: C.text, fontFamily: F.body }}
            >
              Sarah M.
            </div>
          </div>
        </div>
        <div className="flex justify-end gap-2">
          <button
            onClick={close}
            className="px-4 py-2 rounded-lg text-sm"
            style={{ color: C.mut, fontFamily: F.body }}
          >
            Cancelar
          </button>
          <button
            onClick={close}
            className="px-4 py-2 rounded-lg text-sm"
            style={{ background: C.blue, color: "#0D1420", fontFamily: F.body, fontWeight: 600 }}
          >
            Importar e processar
          </button>
        </div>
      </div>
    </div>
  );
}

/* ── shell ── */

export default function App() {
  const [screen, setScreen] = useState("library");
  const [lesson, setLesson] = useState(null);
  const [query, setQuery] = useState("");
  const [pending, setPending] = useState(true);
  const [showImport, setShowImport] = useState(false);

  const NAV = [
    ["library", "Biblioteca", "▤"],
    ["progress", "Progresso", "◔"],
    ["queue", "Fila", "≡"],
  ];

  return (
    <div
      className="w-full min-h-screen flex"
      style={{ background: C.bg, fontFamily: F.body }}
    >
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=Sora:wght@400;600;700&family=Inter:wght@400;600&family=JetBrains+Mono:wght@400&display=swap');
        button:focus-visible { outline: 2px solid ${C.blue}; outline-offset: 2px; }
        ::placeholder { color: ${C.mut}; opacity: .7; }
      `}</style>

      {/* sidebar */}
      <aside
        className="w-52 shrink-0 flex flex-col p-4"
        style={{ borderRight: `1px solid ${C.line}`, background: C.surface }}
      >
        <div className="mb-8 px-2">
          <div
            className="text-base"
            style={{ fontFamily: F.display, color: C.text, fontWeight: 700 }}
          >
            Replay
          </div>
          <div className="text-xs" style={{ color: C.mut }}>
            diário de aulas de inglês
          </div>
        </div>
        {NAV.map(([k, label, icon]) => (
          <button
            key={k}
            onClick={() => {
              setScreen(k);
              setLesson(null);
            }}
            className="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm mb-1 text-left"
            style={{
              background: screen === k && !lesson ? C.surface2 : "transparent",
              color: screen === k && !lesson ? C.text : C.mut,
              fontWeight: screen === k ? 600 : 400,
            }}
          >
            <span>{icon}</span>
            {label}
            {k === "queue" && (
              <span
                className="ml-auto text-xs px-1.5 rounded-full"
                style={{ background: "rgba(110,168,254,.15)", color: C.blue }}
              >
                1
              </span>
            )}
          </button>
        ))}
        <div className="mt-auto px-2 text-xs leading-relaxed" style={{ color: C.mut }}>
          Raiz do Drive:
          <div style={{ fontFamily: F.mono, color: C.text, opacity: 0.7 }}>
            ~/GDrive/Cambly
          </div>
        </div>
      </aside>

      {/* main */}
      <main className="flex-1 flex flex-col min-w-0">
        <header
          className="flex justify-end items-center px-6 py-3"
          style={{ borderBottom: `1px solid ${C.line}` }}
        >
          <SyncPill pending={pending} onPush={() => setPending(false)} />
        </header>

        {lesson ? (
          <LessonDetail lesson={lesson} back={() => setLesson(null)} />
        ) : screen === "library" ? (
          <Library
            open={(l) => {
              setLesson(l);
              setPending(true);
            }}
            onImport={() => setShowImport(true)}
            query={query}
            setQuery={setQuery}
          />
        ) : screen === "progress" ? (
          <Progress />
        ) : (
          <Queue />
        )}
      </main>

      {showImport && <ImportModal close={() => setShowImport(false)} />}
    </div>
  );
}
