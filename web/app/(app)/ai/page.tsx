"use client";

import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Badge, Card, Empty, Heading, LoadError, Loading, Notice } from "@/components/ui";
import { useSession } from "@/components/session";

interface Source { id: string; key: string; title: string; status: string; due_on: string | null; project: { name: string } }
interface Answer { policy_blocked?: boolean; question: string; intent: string; answer: string; method: string; scope: string; as_of: string; sources: Source[] }
interface HistoryItem { id: string; question: string; intent: string; source_count: number; created_at: string }

const suggestions = ["What is blocked?", "What is overdue?", "My highest priorities", "Who is overloaded?", "Summarize the portfolio"];

export default function AIPage() {
  const { activeOrg, can, refreshKey, refresh, toast, openTask } = useSession();
  const allowed = can("ai.query");
  const base = activeOrg ? `/organizations/${activeOrg.id}` : null;
  const history = useApi<List<HistoryItem>>(base && allowed ? `${base}/ai/history` : null, refreshKey);
  const policy = useApi<{ policy: { enabled: boolean } }>(base && allowed ? `${base}/ai/policy` : null, refreshKey);
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState<Answer | null>(null);
  const [asking, setAsking] = useState(false);

  if (!activeOrg) return <Empty title="No organization selected" desc="The AI Intelligence Center works inside an organization. Switch to one from the workspace menu." />;
  if (!allowed) return <Empty title="Not available for your role" desc="The AI Intelligence Center is available to organization owners and executives." />;
  if (history.error) return <LoadError message={history.error.message} onRetry={history.reload} />;
  if (!history.data || !policy.data) return <Loading />;
  if (!policy.data.policy.enabled) return <Empty title="AI is turned off" desc="Your organization's AI policy has turned off the AI Intelligence Center. An owner or admin can turn it on in Settings." />;

  async function ask(q: string) {
    if (!q.trim()) return;
    setAsking(true);
    try {
      setAnswer(await api<Answer>(`${base}/ai/ask`, { method: "POST", body: { question: q } }));
      setQuestion("");
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
    setAsking(false);
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    ask(question);
  }

  return (
    <>
      <Heading title="A little intelligence. A lot more clarity." sub="Context you can trace. Suggestions you can review. Decisions that stay with you." />
      <Notice>Answers are computed by transparent rules from organization work you can already access. No language model is connected, private work is never included, and nothing is changed on your behalf.</Notice>
      <div className="ai-layout">
        <div>
          <section className="card ai-hero">
            <div className="ai-orb"><Icon name="sparkles" className="lg" /></div>
            <div className="eyebrow" style={{ marginTop: 20 }}>YOUR THOUGHT PARTNER, IN CONTEXT</div>
            <h1>See what matters.<br />Make the next move clearer.</h1>
            <p>Explore priorities, dependencies and risk across {activeOrg.name}.</p>
            <form className="ai-prompt" onSubmit={submit}>
              <input value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="What needs my attention this week?" aria-label="Ask TaskForce" maxLength={300} required />
              <button className="btn primary" type="submit" disabled={asking}>{asking ? "Thinking…" : "Ask TaskForce"} <Icon name="arrow" className="sm" /></button>
            </form>
            <div className="suggestions">
              {suggestions.map((q) => <button key={q} onClick={() => ask(q)} disabled={asking}>{q}</button>)}
            </div>
          </section>
          {answer && (
            <section className="ai-answer">
              <div className="row between"><h3>{answer.question}</h3><Badge label={answer.policy_blocked ? "Blocked by AI policy" : "Rules-based answer"} tone={answer.policy_blocked ? "orange" : "gray"} /></div>
              <p style={{ margin: "15px 0" }}>{answer.answer}</p>
              <div className="tiny muted" style={{ marginBottom: 7 }}>SOURCE RECORDS · {answer.scope}</div>
              {answer.sources.length ? (
                answer.sources.map((s) => (
                  <div key={s.id} style={{ margin: "10px 0" }}>
                    <button className="textbtn" onClick={() => openTask(s.id)}>
                      <Icon name="checkSquare" className="sm" /> {s.key} · {s.title}
                    </button>
                    <span className="tiny muted"> — {s.project.name}{s.due_on ? ` · due ${fmtDate(s.due_on)}` : ""}</span>
                  </div>
                ))
              ) : (
                <p>No matching records.</p>
              )}
              <p className="tiny muted" style={{ marginTop: 20 }}>Method: {answer.method} · As of {fmtDate(answer.as_of)}</p>
            </section>
          )}
        </div>
        <Card title="Your recent questions">
          {history.data.items.length ? (
            history.data.items.map((h) => (
              <div className="settings-row" key={h.id}>
                <div>
                  <button className="task-title" onClick={() => ask(h.question)} disabled={asking}>{h.question}</button>
                  <p>{h.intent.toLowerCase()} · {h.source_count} source{h.source_count === 1 ? "" : "s"} · {new Date(h.created_at).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" })}</p>
                </div>
              </div>
            ))
          ) : (
            <p>Questions you ask appear here. Only you can see them.</p>
          )}
        </Card>
      </div>
    </>
  );
}
