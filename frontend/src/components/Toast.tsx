/**
 * Toast — minimal fixed-position notification for user feedback.
 * Renders at bottom-right with slide-in animation and auto-dismiss.
 */
import { useEffect, useState } from "react";

export interface ToastProps {
  message: string;
  type: "success" | "error";
  onDismiss: () => void;
}

export default function Toast({ message, type, onDismiss }: ToastProps) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    // Trigger slide-in on next frame so the transition fires.
    const frame = requestAnimationFrame(() => setVisible(true));

    const timer = setTimeout(() => {
      setVisible(false);
      // Wait for the fade-out transition before unmounting.
      setTimeout(onDismiss, 300);
    }, 3_000);

    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(timer);
    };
  }, [onDismiss]);

  const bg = type === "success"
    ? "bg-green-600/90 border-green-500"
    : "bg-red-600/90 border-red-500";

  return (
    <div
      role="status"
      aria-live="polite"
      className={`fixed bottom-6 right-6 z-50 px-4 py-3 rounded-lg border text-sm text-white shadow-lg
        transition-all duration-300 ease-in-out
        ${bg}
        ${visible ? "translate-y-0 opacity-100" : "translate-y-4 opacity-0"}`}
    >
      {message}
    </div>
  );
}
