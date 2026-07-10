import React from "react";
import ReactDOM from "react-dom/client";
import { FlueProvider } from "@flue/react";
import { createFlueClient } from "@flue/sdk";
import App from "@/App";
import { ThemeProvider } from "@/theme";
import "@/globals.css";

let root = document.getElementById("root");
if (!root) {
  root = document.createElement("div");
  root.id = "root";
  document.body.appendChild(root);
}

// Same-origin: the Go backend reverse-proxies /agents/* to the Flue sidecar.
const flueClient = createFlueClient({ baseUrl: "/" });

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <ThemeProvider>
      <FlueProvider client={flueClient}>
        <App />
      </FlueProvider>
    </ThemeProvider>
  </React.StrictMode>,
);
