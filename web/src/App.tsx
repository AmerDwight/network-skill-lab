import { BrowserRouter, Route, Routes } from "react-router";

import { AuthGate } from "./components/AuthGate";
import { AdminPage } from "./pages/AdminPage";
import { AttemptPage } from "./pages/AttemptPage";
import { AttemptResultPage } from "./pages/AttemptResultPage";
import { DocListPage } from "./pages/DocListPage";
import { DocPage } from "./pages/DocPage";
import { LabDetailPage } from "./pages/LabDetailPage";
import { LabListPage } from "./pages/LabListPage";
import { LoginPage } from "./pages/LoginPage";
import { TrackListPage } from "./pages/TrackListPage";
import { TrackPage } from "./pages/TrackPage";

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<AuthGate />}>
          <Route path="/" element={<LabListPage />} />
          <Route path="/labs/:id" element={<LabDetailPage />} />
          <Route path="/docs" element={<DocListPage />} />
          <Route path="/docs/*" element={<DocPage />} />
          <Route path="/tracks" element={<TrackListPage />} />
          <Route path="/tracks/:id" element={<TrackPage />} />
          <Route path="/attempts/:id" element={<AttemptPage />} />
          <Route path="/attempts/:id/result" element={<AttemptResultPage />} />
          <Route path="/admin" element={<AdminPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
