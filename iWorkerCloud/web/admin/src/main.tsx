import React from 'react';
import { createRoot } from 'react-dom/client';
import './i18n';
import './style.css';
import App from './App';

createRoot(document.getElementById('root')!).render(
  <React.StrictMode><App /></React.StrictMode>
);
