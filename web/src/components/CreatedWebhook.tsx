import { Banner } from "./Banner";
import { Button } from "./Button";

export function CreatedWebhook({ path, onDone }: { path: string; onDone: () => void }) {
  const url = new URL(path, window.location.origin).href;

  return (
    <Banner>
      <p>Save this URL now. It is shown only once. Send a POST request to trigger the webhook.</p>
      <input
        className="output webhook-url"
        aria-label="Webhook URL"
        autoComplete="off"
        value={url}
        readOnly
        onFocus={(e) => e.currentTarget.select()}
      />
      <Button variant="text" onClick={onDone}>Done</Button>
    </Banner>
  );
}
