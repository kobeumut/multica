"use client";

import { useState } from "react";
import { useCreateAutoRouter } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { isRuntimeUsableForUser } from "@multica/core/runtimes";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { RuntimePicker } from "./runtime-picker";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const MODEL_PULL_COMMAND = "ollama pull d1-omni:600m";

export function AutoRouterButton({ runtimes, members, currentUserId }: {
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  currentUserId: string | null;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [runtimeId, setRuntimeId] = useState("");
  const wsId = useWorkspaceId();
  const create = useCreateAutoRouter(wsId);
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const usable = runtimes.some((runtime) => runtime.id === runtimeId && isRuntimeUsableForUser(runtime, currentUserId));

  async function submit() {
    try {
      const agent = await create.mutateAsync(runtimeId);
      setOpen(false);
      navigation.push(paths.agentDetail(agent.id));
    } catch {
      // The mutation error remains visible and selection is kept for retry.
    }
  }

  return (
    <Dialog open={open} onOpenChange={(value) => { if (!create.isPending) setOpen(value); }}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        {t(($) => $.autorouter.add)}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.autorouter.add)}</DialogTitle>
          <DialogDescription>{t(($) => $.autorouter.local_requirement)}</DialogDescription>
        </DialogHeader>
        <code className="block break-all rounded bg-muted p-3 text-caption">{MODEL_PULL_COMMAND}</code>
        <RuntimePicker runtimes={runtimes} members={members} currentUserId={currentUserId} selectedRuntimeId={runtimeId} onSelect={setRuntimeId} disabled={create.isPending} />
        {create.error && <p role="alert" className="break-words text-sm text-destructive">{create.error.message}</p>}
        <DialogFooter>
          <Button disabled={!usable || create.isPending} aria-busy={create.isPending} onClick={submit}>{t(($) => $.autorouter.add)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
