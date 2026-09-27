import { createRoot } from 'react-dom/client';
import { SidePanel } from './ui/SidePanel';
import './ui/tokens.css';
import './ui/globals.css';

const container = document.getElementById('root');
if (!container) {
  throw new Error('Missing required element: root');
}

createRoot(container).render(<SidePanel />);
