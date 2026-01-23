import { Capture, useCaptures } from "@b4.capture";
import {
  CaptureIcon,
  ClearIcon,
  CopyIcon,
  DownloadIcon,
  RefreshIcon,
  SuccessIcon,
  UploadIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import { Field, FieldDescription, FieldLabel } from "@primitives/field";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";
import { Spinner } from "@primitives/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
import { cn } from "@design/lib/utils";
import { useEffect, useState } from "react";
import {
  Empty,
  EmptyDescription,
  EmptyMedia,
  EmptyTitle,
} from "@design/primitives/empty";

export const CaptureSettings = () => {
  const { showError, showSuccess } = useSnackbar();
  const [probeForm, setProbeForm] = useState({ domain: "" });
  const [uploadForm, setUploadForm] = useState<{
    domain: string;
    file: File | null;
  }>({ domain: "", file: null });
  const [countdown, setCountdown] = useState<number | null>(null);

  const {
    captures,
    loading,
    loadCaptures,
    probe,
    deleteCapture,
    clearAll,
    upload,
    download,
  } = useCaptures();

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  useEffect(() => {
    if (!uploadForm.domain && uploadForm.file) {
      setUploadForm((prev) => ({ ...prev, domain: prev.file?.name ?? "" }));
    }
  }, [uploadForm]);

  const probeCapture = async () => {
    if (!probeForm.domain) return;

    const capturedDomain = probeForm.domain.toLowerCase().trim();

    setCountdown(30);
    const countdownInterval = setInterval(() => {
      setCountdown((prev) => {
        if (prev === null || prev <= 1) {
          clearInterval(countdownInterval);
          return null;
        }
        return prev - 1;
      });
    }, 1000);

    try {
      const result = await probe(capturedDomain, "tls");
      clearInterval(countdownInterval);
      setCountdown(null);

      if (result.already_captured) {
        showSuccess(`Already have payload for ${capturedDomain}`);
      } else if (captures.some((c) => c.domain === capturedDomain)) {
        showSuccess(`Captured payload for ${capturedDomain}`);
        setProbeForm({ domain: "" });
      } else {
        showError(`Capture timed out for ${capturedDomain}`);
      }
    } catch (error) {
      clearInterval(countdownInterval);
      setCountdown(null);
      console.error("Failed to probe:", error);
      showError("Failed to initiate capture");
    }
  };

  const handleDelete = async (capture: Capture) => {
    try {
      await deleteCapture(capture.protocol, capture.domain);
      showSuccess(`Deleted ${capture.domain}`);
    } catch {
      showError("Failed to delete capture");
    }
  };

  const handleClear = async () => {
    if (!confirm("Delete all captured payloads?")) return;
    try {
      await clearAll();
      showSuccess("All captures cleared");
    } catch {
      showError("Failed to clear captures");
    }
  };

  const [hexDialog, setHexDialog] = useState<{
    open: boolean;
    capture: Capture | null;
  }>({ open: false, capture: null });

  const uploadCapture = async () => {
    if (!uploadForm.file || !uploadForm.domain) return;

    try {
      await upload(uploadForm.file, uploadForm.domain.toLowerCase(), "tls");
      showSuccess(`Uploaded payload for ${uploadForm.domain}`);
      setUploadForm({ domain: "", file: null });
    } catch {
      showError("Failed to upload file");
    }
  };

  const copyHex = (hexData: string) => {
    void navigator.clipboard.writeText(hexData);
    showSuccess("Hex data copied to clipboard");
  };

  return (
    <div className="space-y-6">
      {/* Upload + Capture side by side */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <UploadIcon />
              Upload Custom Payload
            </CardTitle>
            <CardDescription>
              Upload your own binary payload file (max 64KB)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Field>
              <FieldLabel>Name/Domain</FieldLabel>
              <Input
                value={uploadForm.domain}
                onChange={(e) =>
                  setUploadForm({
                    ...uploadForm,
                    domain: e.target.value.toLowerCase(),
                  })
                }
                placeholder="youtube.com"
                disabled={loading}
              />
              <FieldDescription>
                Name associated with the uploaded payload
              </FieldDescription>
            </Field>
          </CardContent>
          <CardFooter>
            <Button variant="outline" disabled={loading} className="shrink-0">
              <label>
                {uploadForm.file ? uploadForm.file.name : "Choose File..."}
                <input
                  type="file"
                  className="hidden"
                  accept=".bin,application/octet-stream"
                  onChange={(e) => {
                    const file = e.target.files?.[0] || null;
                    setUploadForm({ ...uploadForm, file });
                  }}
                />
              </label>
            </Button>
            {uploadForm.file && (
              <p className="text-muted-foreground ml-auto text-xs">
                {uploadForm.file.size} bytes
              </p>
            )}
            <Button
              onClick={() => void uploadCapture()}
              disabled={loading || !uploadForm.file || !uploadForm.domain}
              className="ml-auto"
            >
              {loading ? (
                <>
                  <Spinner />
                  Uploading...
                </>
              ) : (
                <>
                  <UploadIcon />
                  Upload
                </>
              )}
            </Button>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <CaptureIcon />
              Capture Payload
            </CardTitle>
            <CardDescription>
              Probe domain to capture its TLS ClientHello
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Field>
              <FieldLabel>Domain</FieldLabel>
              <Input
                value={probeForm.domain}
                onChange={(e) =>
                  setProbeForm({ domain: e.target.value.toLowerCase() })
                }
                onKeyPress={(e) => {
                  if (e.key === "Enter" && !loading && probeForm.domain) {
                    void probeCapture();
                  }
                }}
                placeholder="youtube.com"
                disabled={loading}
              />
              <FieldDescription>Enter domain to capture from</FieldDescription>
            </Field>
          </CardContent>
          <CardFooter>
            <Tooltip>
              <TooltipTrigger>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => void loadCaptures()}
                  disabled={loading}
                >
                  <RefreshIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Refresh list</p>
              </TooltipContent>
            </Tooltip>
            <Button
              onClick={() => void probeCapture()}
              disabled={loading || !probeForm.domain}
              className="ml-auto"
            >
              {loading ? (
                <>
                  <Spinner />
                  Capturing...
                </>
              ) : (
                <>
                  <CaptureIcon />
                  Capture
                </>
              )}
            </Button>

            {captures.length > 0 && (
              <Tooltip>
                <TooltipTrigger>
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={() => void handleClear()}
                    disabled={loading}
                  >
                    <ClearIcon />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Clear all captures</p>
                </TooltipContent>
              </Tooltip>
            )}
          </CardFooter>
        </Card>
      </div>

      {loading && countdown !== null && (
        <Alert>
          <AlertTitle>Capture window is open for {probeForm.domain}</AlertTitle>
          <AlertAction>
            <Badge className="ml-auto">{`${countdown}s`}</Badge>
          </AlertAction>
          <AlertDescription>
            <ul>
              <li>
                Visit{" "}
                <a
                  href={`https://${probeForm.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  https://{probeForm.domain}
                </a>
              </li>
              <li>
                Or run:{" "}
                <code>curl -o /dev/null -s https://{probeForm.domain}</code> in
                your terminal
              </li>
            </ul>
          </AlertDescription>
        </Alert>
      )}

      {/* Captured Payloads - Flat grid like SetCards */}
      {captures.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <DownloadIcon />
              Captured Payloads
            </CardTitle>

            <CardDescription>
              {captures.length} payload{captures.length !== 1 ? "s" : ""} ready
              for use
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-4">
              {captures.map((capture) => (
                <CaptureCard
                  key={`${capture.protocol}:${capture.domain}`}
                  capture={capture}
                  onViewHex={() => setHexDialog({ open: true, capture })}
                  onDownload={() => download(capture)}
                  onDelete={() => void handleDelete(capture)}
                />
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Empty State */}
      {captures.length === 0 && !loading && (
        <Empty className="border">
          <EmptyMedia variant="icon">
            <CaptureIcon />
          </EmptyMedia>
          <EmptyTitle>No captured payloads yet</EmptyTitle>
          <EmptyDescription>
            Enter a domain above and click Capture to get started
          </EmptyDescription>
        </Empty>
      )}

      {/* Hex Dialog */}
      <Dialog
        open={hexDialog.open}
        onOpenChange={(open) =>
          !open && setHexDialog({ open: false, capture: null })
        }
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="bg-accent text-accent-foreground flex size-10 items-center justify-center rounded-md">
                <CaptureIcon />
              </div>
              <div className="flex-1">
                <DialogTitle>Payload Hex Data</DialogTitle>
                <DialogDescription className="mt-1">
                  Copy for use in Faking → Custom Payload
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <div className="py-4">
            {hexDialog.capture && (
              <div className="space-y-4">
                <Alert>
                  <SuccessIcon className="h-3.5 w-3.5" />
                  <AlertDescription>
                    TLS payload for {hexDialog.capture.domain} •{" "}
                    {hexDialog.capture.size} bytes
                  </AlertDescription>
                </Alert>
                <div className="bg-muted max-h-100 overflow-auto rounded-md p-4 font-mono text-xs break-all select-all">
                  {hexDialog.capture.hex_data}
                </div>
              </div>
            )}
          </div>
          <Separator />
          <DialogFooter>
            <Button
              onClick={() => {
                if (hexDialog.capture?.hex_data) {
                  copyHex(hexDialog.capture.hex_data);
                }
                setHexDialog({ open: false, capture: null });
              }}
            >
              Copy & Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

// Card component styled like SetCard
interface CaptureCardProps {
  capture: Capture;
  onViewHex: () => void;
  onDownload: () => void;
  onDelete: () => void;
}

const CaptureCard = ({
  capture,
  onViewHex,
  onDownload,
  onDelete,
}: CaptureCardProps) => {
  return (
    <Card className="border-border hover:border-secondary flex h-full cursor-pointer flex-col border p-4 transition-all hover:-translate-y-0.5 hover:shadow-lg">
      {/* Header */}
      <div className="mb-2 flex flex-row items-start justify-between">
        <div className="min-w-0 flex-1">
          <h6 className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
            {capture.domain}
          </h6>
          <p className="text-muted-foreground text-xs">
            {capture.size.toLocaleString()} bytes
          </p>
        </div>
        <CaptureIcon className="text-secondary ml-2 size-5 shrink-0" />
      </div>

      {/* Timestamp */}
      <p className="text-muted-foreground mb-4 text-xs">
        {new Date(capture.timestamp).toLocaleString()}
      </p>

      {/* Spacer */}
      <div className="flex-1" />

      {/* Actions */}
      <div className="border-border flex flex-row gap-1 border-t pt-4">
        <Tooltip>
          <TooltipTrigger>
            <Button size="sm" variant="ghost" onClick={onViewHex}>
              <CopyIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>View/Copy hex</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger>
            <Button size="sm" variant="ghost" onClick={onDownload}>
              <DownloadIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Download .bin</p>
          </TooltipContent>
        </Tooltip>
        <div className="flex-1" />
        <Tooltip>
          <TooltipTrigger>
            <Button size="sm" variant="ghost" onClick={onDelete}>
              <ClearIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Delete</p>
          </TooltipContent>
        </Tooltip>
      </div>
    </Card>
  );
};
