"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { IconButton } from "./ui";

export function Modal({ title, onClose, wide, children }: { title: string; onClose: () => void; wide?: boolean; children: ReactNode }) {
  const ref = useRef<HTMLElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("input:not([type=hidden]),select,textarea,button")?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, []);
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <section ref={ref} role="dialog" aria-modal="true" aria-label={title} className={`modal ${wide ? "wide" : ""}`}>
        <div className="modal-head">
          <h2>{title}</h2>
          <IconButton label="Close dialog" icon="close" onClick={onClose} />
        </div>
        {children}
      </section>
    </div>
  );
}
