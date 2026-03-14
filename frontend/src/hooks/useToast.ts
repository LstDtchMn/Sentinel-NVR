/**
 * useToast — lightweight hook for triggering toast notifications.
 * Returns the current toast state and a showToast function.
 * Auto-dismisses after the Toast component's 3-second timer fires.
 */
import { useState, useCallback } from "react";

export interface ToastState {
  message: string;
  type: "success" | "error";
}

export function useToast() {
  const [toast, setToast] = useState<ToastState | null>(null);

  const showToast = useCallback((message: string, type: "success" | "error") => {
    setToast({ message, type });
  }, []);

  const dismissToast = useCallback(() => {
    setToast(null);
  }, []);

  return { toast, showToast, dismissToast };
}
