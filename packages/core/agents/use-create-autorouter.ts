import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { agentListOptions } from "../workspace/queries";
import { labelListOptions } from "../labels";

export function useCreateAutoRouter(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.createAutoRouterAgent({ runtime_id: runtimeId }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: agentListOptions(wsId).queryKey }),
        queryClient.invalidateQueries({ queryKey: labelListOptions(wsId, "agent").queryKey }),
      ]);
    },
  });
}
