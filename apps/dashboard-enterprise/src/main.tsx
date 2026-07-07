import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import '@rackops/ui/styles/tokens.css';
import './app.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
