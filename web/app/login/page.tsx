"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { api, errorMessage } from "@/lib/api";

const demoAccounts = [
  ["vinit@example.com", "Vinit Tyagi", "Tenant owner · Acme + Vertex"],
  ["meera.iyer@example.com", "Meera Iyer", "Executive / PMO · Acme"],
  ["arjun.mehta@example.com", "Arjun Mehta", "Project manager · Acme"],
  ["ishaan.rao@example.com", "Ishaan Rao", "Member · Acme + Vertex"],
];

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState(demoAccounts[0][0]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function signIn(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/dev/login", { method: "POST", body: { email } });
      router.replace("/personal");
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <main style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: 24, background: "var(--canvas)" }}>
      <section className="card" style={{ width: "min(440px, 100%)" }}>
        <div className="row" style={{ marginBottom: 18 }}>
          <span className="brandmark">
            <svg viewBox="0 0 24 28"><path d="m14 1-11 15h8l-2 11L22 11h-9z" /></svg>
          </span>
          <span className="brandtext" style={{ fontSize: 18, fontWeight: 600 }}>TaskForce</span>
        </div>
        <h1 style={{ fontSize: 20, marginBottom: 6 }}>Sign in to your workspace</h1>
        <p style={{ marginBottom: 18 }}>
          Development sign-in. Enterprise single sign-on (OIDC) replaces this screen before any pilot.
        </p>
        <form onSubmit={signIn}>
          <label className="field full">
            Work email
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoComplete="username" />
          </label>
          <div className="field-error" role="alert">{error}</div>
          <button className="btn primary" type="submit" disabled={busy} style={{ width: "100%", justifyContent: "center", marginTop: 8 }}>
            Continue
          </button>
        </form>
        <p className="tiny muted" style={{ margin: "20px 0 8px" }}>FICTIONAL DEMO ACCOUNTS</p>
        {demoAccounts.map(([addr, name, role]) => (
          <button key={addr} className="workspace-choice" onClick={() => setEmail(addr)} type="button">
            <span>
              <h3>{name}</h3>
              <p className="tiny muted">{role}</p>
            </span>
          </button>
        ))}
      </section>
    </main>
  );
}
