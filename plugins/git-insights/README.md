# Git Insights

This intentionally small plugin demonstrates the complete SuperCLI plugin
workflow without requiring compilation:

1. `plugin.yaml` declares identity and API compatibility.
2. Each action is an ordinary executable plus an argument array.
3. `{{workspace_dir}}` points the action at the selected workspace.
4. SuperCLI validates and namespaces the actions as
   `git-insights--status` and `git-insights--recent-commits`.
5. The actions appear in the normal `p` palette as `[Git Insights] ...`.

The installer copies this directory into `SUPERCLI_PLUGIN_DIR` only when it is
not already installed. Editing the installed manifest and restarting SuperCLI
is enough to experiment with the plugin API.
