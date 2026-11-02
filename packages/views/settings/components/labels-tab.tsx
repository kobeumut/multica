"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels";
import { useT } from "../../i18n";
import { LabelManager, type LabelScope } from "../../labels/label-manager";
import { SettingsTab, SettingsViewTabs } from "./settings-layout";

/** Workspace label catalogs; agent labels are AutoRouter's dynamic categories. */
export function LabelsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const [scope, setScope] = useState<LabelScope>("issue");
  // Same queries the manager runs, so the counts come from the shared cache.
  const { data: issueLabels } = useQuery(labelListOptions(wsId, "issue"));
  const { data: skillLabels } = useQuery(labelListOptions(wsId, "skill"));
  const { data: agentLabels } = useQuery(labelListOptions(wsId, "agent"));

  return (
    <SettingsTab
      title={t(($) => $.labels.title)}
      description={t(($) => $.labels.description)}
      scope="workspace"
    >
      <div className="space-y-4">
        <SettingsViewTabs
          label={t(($) => $.labels.title)}
          items={[
            { value: "issue", label: t(($) => $.labels.scopes.issue), count: issueLabels?.length },
            { value: "skill", label: t(($) => $.labels.scopes.skill), count: skillLabels?.length },
            { value: "agent", label: t(($) => $.labels.scopes.agent), count: agentLabels?.length },
          ]}
          value={scope}
          onChange={setScope}
        />
        {/* Keyed so switching lists also clears the search. */}
        <LabelManager key={scope} scope={scope} />
      </div>
    </SettingsTab>
  );
}
