import "@material/web/dialog/dialog.js";
import { useEffect, useRef, type ReactNode } from "react";

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "md-dialog": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & { open?: boolean };
    }
  }
}

type Props = {
  headline: string;
  // Called when the user dismisses the dialog with Escape or a click outside it.
  onDismiss: () => void;
  actions: ReactNode;
  children: ReactNode;
};

// Mount it to open it, unmount it to close it.
export function Dialog({ headline, onDismiss, actions, children }: Props) {
  const ref = useRef<HTMLElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.addEventListener("close", onDismiss);
    return () => el.removeEventListener("close", onDismiss);
  }, [onDismiss]);

  return (
    <md-dialog ref={ref} open>
      <div slot="headline">{headline}</div>
      <div slot="content">{children}</div>
      <div slot="actions">{actions}</div>
    </md-dialog>
  );
}
