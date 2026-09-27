import type { ReactNode } from 'react';
import styles from './Card.module.css';

// Elevated glass card with a specular top rim, per the elevation recipe.
export function Card({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`${styles.card}${className ? ` ${className}` : ''}`}>
      {children}
    </div>
  );
}
