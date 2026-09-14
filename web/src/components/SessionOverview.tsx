type SessionOverviewProps = {
  total: number | null;
  loaded: number;
  active: number;
};

export function SessionOverview({
  total,
  loaded,
  active,
}: SessionOverviewProps) {
  return (
    <section
      className="session-overview"
      aria-labelledby="session-overview-title"
    >
      <div className="session-overview-copy">
        <p className="session-kicker">ORAHERMES / WORKSPACE</p>
        <h2 id="session-overview-title">
          Your agent.
          <br />
          <em>Its working memory.</em>
        </h2>
        <p>
          Pick up a conversation. Inspect the tools behind it.
          <br />
          Keep the context that makes the next step possible.
        </p>
      </div>
      <dl className="session-ledger">
        <div>
          <dt>Stored sessions</dt>
          <dd>{total === null ? "—" : total.toLocaleString()}</dd>
        </div>
        <div>
          <dt>On this page</dt>
          <dd>{total === null ? "—" : loaded.toLocaleString()}</dd>
        </div>
        <div>
          <dt>Active on this page</dt>
          <dd>{total === null ? "—" : active.toLocaleString()}</dd>
        </div>
      </dl>
    </section>
  );
}
