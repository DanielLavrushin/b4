import { DiscoveryRunner } from "./Discovery";

export function DiscoveryPage() {
  return (
    <div className="h-full flex flex-col overflow-auto">
      <div className="flex flex-col gap-6">
        <DiscoveryRunner />
      </div>
    </div>
  );
}
