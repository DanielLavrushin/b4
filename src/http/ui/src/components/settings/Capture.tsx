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
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { cn } from "@design/lib/utils";
import { useEffect, useState } from "react";

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
      {/* Info */}
      <Alert>
        <CaptureIcon className="h-3.5 w-3.5" />
        <AlertDescription>
          <h6 className="mb-2 text-sm font-semibold">
            Capture real TLS ClientHello for custom payload generation
          </h6>
          <p className="text-muted-foreground text-xs">
            One capture per domain. Use in Faking → Captured Payload
          </p>
        </AlertDescription>
      </Alert>

      {/* Upload + Capture side by side */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <UploadIcon />
              <CardTitle>Upload Custom Payload</CardTitle>
            </div>
            <CardDescription>
              Upload your own binary payload file (max 64KB)
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
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
              <div className="flex flex-row items-center gap-2">
                <Button
                  variant="outline"
                  disabled={loading}
                  className="shrink-0"
                  asChild
                >
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
                  <p className="text-muted-foreground text-xs">
                    {uploadForm.file.size} bytes
                  </p>
                )}
                <Button
                  onClick={() => void uploadCapture()}
                  disabled={loading || !uploadForm.file || !uploadForm.domain}
                >
                  {loading ? (
                    <>
                      <Spinner className="mr-2 h-4 w-4" />
                      Uploading...
                    </>
                  ) : (
                    <>
                      <UploadIcon className="mr-2 h-4 w-4" />
                      Upload
                    </>
                  )}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CaptureIcon />
              <CardTitle>Capture Payload</CardTitle>
            </div>
            <CardDescription>
              Probe domain to capture its TLS ClientHello
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
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
                <FieldDescription>
                  Enter domain to capture from
                </FieldDescription>
              </Field>
              <div className="flex flex-row gap-2">
                <Button
                  className="flex-1"
                  onClick={() => void probeCapture()}
                  disabled={loading || !probeForm.domain}
                >
                  {loading ? (
                    <>
                      <Spinner className="mr-2 h-4 w-4" />
                      Capturing...
                    </>
                  ) : (
                    <>
                      <CaptureIcon className="mr-2 h-4 w-4" />
                      Capture
                    </>
                  )}
                </Button>
                <Tooltip>
                  <TooltipTrigger asChild>
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
                {captures.length > 0 && (
                  <Tooltip>
                    <TooltipTrigger asChild>
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
              </div>
              {loading && countdown !== null && (
                <Alert>
                  <AlertDescription>
                    <h6 className="mb-2 text-sm font-semibold">
                      Capture window is open for {probeForm.domain}
                    </h6>
                    <div className="flex flex-row items-center gap-2">
                      <p className="text-xs">
                        Visit{" "}
                        <a
                          href={`https://${probeForm.domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-secondary hover:underline"
                        >
                          https://{probeForm.domain}
                        </a>
                      </p>
                      <Badge
                        className={cn(
                          "min-w-12 px-1.5 py-0.5 text-xs font-semibold",
                          countdown <= 10
                            ? "bg-accent text-accent-foreground"
                            : "",
                        )}
                      >
                        {`${countdown}s`}
                      </Badge>
                    </div>
                    <p className="text-muted-foreground mt-2 text-xs">
                      Or run:{" "}
                      <code className="text-secondary">
                        curl -o /dev/null -s https://{probeForm.domain}
                      </code>{" "}
                      in your terminal
                    </p>
                  </AlertDescription>
                </Alert>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Captured Payloads - Flat grid like SetCards */}
      {captures.length > 0 && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <DownloadIcon />
              <CardTitle>Captured Payloads</CardTitle>
            </div>
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
        <div className="border-border rounded-md border border-dashed p-8 text-center">
          <CaptureIcon className="text-muted-foreground mx-auto mb-4 h-12 w-12" />
          <h6 className="text-muted-foreground mb-2 text-lg font-semibold">
            No captured payloads yet
          </h6>
          <p className="text-muted-foreground text-sm">
            Enter a domain above and click Capture to get started
          </p>
        </div>
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
              <div className="bg-accent text-accent-foreground flex h-10 w-10 items-center justify-center rounded-md">
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
        <CaptureIcon className="text-secondary ml-2 h-5 w-5 shrink-0" />
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
          <TooltipTrigger asChild>
            <Button size="sm" variant="ghost" onClick={onViewHex}>
              <CopyIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>View/Copy hex</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
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
          <TooltipTrigger asChild>
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
