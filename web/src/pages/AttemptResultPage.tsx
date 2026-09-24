import { useState } from "react";
import { useParams } from "react-router";

import { ResultView } from "../components/ResultView";

export function AttemptResultPage() {
  const { id } = useParams();
  const attemptId = id ?? "";
  const [retries, setRetries] = useState(0);

  return (
    <main>
      <ResultView
        key={`${attemptId}:${retries}`}
        attemptId={attemptId}
        onRetry={() => setRetries((value) => value + 1)}
      />
    </main>
  );
}
