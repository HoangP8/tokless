// Single registration point for all tools.
import { registerTool } from "../core/registry.js";
import rtk from "./rtk.js";
import caveman from "./caveman.js";
import codegraph from "./codegraph.js";
import contextMode from "./context-mode.js";

registerTool(rtk);
registerTool(caveman);
registerTool(codegraph);
registerTool(contextMode);
