import { BrowserRouter, Route, Routes } from "react-router";

import { AttemptPage } from "./pages/AttemptPage";
import { AttemptResultPage } from "./pages/AttemptResultPage";
import { LabListPage } from "./pages/LabListPage";

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<LabListPage />} />
        <Route path="/attempts/:id" element={<AttemptPage />} />
        <Route path="/attempts/:id/result" element={<AttemptResultPage />} />
      </Routes>
    </BrowserRouter>
  );
}
