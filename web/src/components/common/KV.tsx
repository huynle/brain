/**
 * KV — matches wireframe `.kv-grid` (2-column key/value grid).
 */
import React from "react";

export interface KVPair {
  k: React.ReactNode;
  v: React.ReactNode;
}

export interface KVProps {
  pairs?: KVPair[];
  children?: React.ReactNode;
  className?: string;
}

export function KV({ pairs, children, className }: KVProps): JSX.Element {
  const cls = className ? "kv-grid " + className : "kv-grid";
  return (
    <div className={cls}>
      {pairs?.map((p, i) => (
        <React.Fragment key={i}>
          <div className="k">{p.k}</div>
          <div className="v">{p.v}</div>
        </React.Fragment>
      ))}
      {children}
    </div>
  );
}
