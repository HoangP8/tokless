// Single registration point for all agents.

import { registerAgent } from "../core/registry.js";
import claude from "./claude.js";
import opencode from "./opencode.js";
import codex from "./codex.js";

registerAgent(claude);
registerAgent(opencode);
registerAgent(codex);
