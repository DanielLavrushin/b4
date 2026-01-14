import { useState, useCallback } from "react";
import { useSnackbar } from "@context/SnackbarProvider";

interface IpModalState {
  open: boolean;
  ip: string;
  variants: string[];
  selected: string | string[];
}

export function useIpActions() {
  const { showSuccess, showError } = useSnackbar();
  const [modalState, setModalState] = useState<IpModalState>({
    open: false,
    ip: "",
    variants: [],
    selected: "",
  });

  const openModal = useCallback((ip: string, variants: string[]) => {
    ip = ip.split(":")[0]; // Remove port if present

    setModalState({
      open: true,
      ip,
      variants,
      selected: variants[0] || ip,
    });
  }, []);

  const closeModal = useCallback(() => {
    setModalState({
      open: false,
      ip: "",
      variants: [],
      selected: [] as string[],
    });
  }, []);

  const selectVariant = useCallback((variant: string | string[]) => {
    setModalState((prev) => ({ ...prev, selected: variant }));
  }, []);

  const addIp = useCallback(
    async (setId: string, setName?: string) => {
      if (!modalState.selected) return;

      try {
        const response = await fetch("/api/geoip", {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            cidr: Array.isArray(modalState.selected)
              ? modalState.selected
              : [modalState.selected],
            set_id: setId,
            set_name: setName,
          }),
        });

        if (response.ok) {
          showSuccess(
            `${
              Array.isArray(modalState.selected)
                ? modalState.selected.length + " entries"
                : modalState.selected
            } added successfully`,
          );
          closeModal();
        } else {
          const error = (await response.json()) as { message: string };
          showError(`Failed to add ip: ${error.message}`);
        }
      } catch (error) {
        showError(`Failed to add ip: ${String(error)}`);
      }
    },
    [modalState.selected, closeModal, showError, showSuccess],
  );

  return {
    modalState,
    openModal,
    closeModal,
    selectVariant,
    addIp,
  };
}
