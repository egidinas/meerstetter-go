import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
// eslint-disable-next-line @typescript-eslint/ban-ts-comment
// @ts-ignore
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("No #root element");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>
);
