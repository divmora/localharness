import { useState, useEffect, useCallback, useRef } from 'react';
import { invoke } from '@tauri-apps/api/core';
import WebSocket from '@tauri-apps/plugin-websocket';
import { create, toBinary, fromBinary } from '@bufbuild/protobuf';
import { homeDir } from '@tauri-apps/api/path';
import { 
    ClientMessageSchema,
    ServerMessageSchema,
    UserMessageSchema,
    StepUpdate,
    StepUpdateSchema,
    StepUpdate_Source,
    StepUpdate_State,
    InitRequestSchema,
    HarnessConfigSchema,
    QuestionResponseSchema,
    PermissionResponseSchema,
    InterruptRequestSchema,
    ResumeRequestSchema,
    TrajectoryState_TrajState
} from '../gen/localharness/v1/localharness_pb';



interface HarnessConnection {
    port: number;
    api_key: string;
}

export function useHarness(activeSessionId: string | null, workspacePath?: string | null, initialBudget?: number, isManager: boolean = false, onSessionCreated?: (sessionId: string) => void, activeOfficeId?: string | null) {
    const [connected, setConnected] = useState(false);
    const [connectionError, setConnectionError] = useState<string | null>(null);
    const [serverReady, setServerReady] = useState(false);
    const [socket, setSocket] = useState<WebSocket | null>(null);
    const [steps, setSteps] = useState<StepUpdate[]>([]);
    const [trajectoryState, setTrajectoryState] = useState<TrajectoryState_TrajState>(TrajectoryState_TrajState.TRAJ_UNSPECIFIED);
    const messageQueueRef = useRef<number[][]>([]);

    useEffect(() => {
        let ws: WebSocket | null = null;
        
        if (!activeSessionId) {
            setConnected(false);
            setConnectionError(null);
            setSteps([]);
            setSocket(null);
            setServerReady(false);
            setTrajectoryState(TrajectoryState_TrajState.TRAJ_UNSPECIFIED);
            return;
        }

        async function initHarness() {
            setConnectionError(null);
            setSteps([]);
            setServerReady(false);
            setTrajectoryState(TrajectoryState_TrajState.TRAJ_UNSPECIFIED);
            
            let conn: HarnessConnection | null = null;
            
            try {
                console.log("Requesting sidecar from Rust...");
                try {
                    conn = await invoke<HarnessConnection>('start_harness', { 
                        target: null,
                        sessionId: activeSessionId || "temp-session"
                    });
                    console.log("Got sidecar port:", conn.port);
                } catch (err: any) {
                    if (err.toString().includes('__TAURI_INTERNALS__')) {
                        console.warn("Tauri not detected, falling back to standalone localhost:4000 (web dev mode)");
                        conn = { port: 4000, api_key: "dev-key" };
                    } else {
                        setConnectionError(err.toString());
                        return;
                    }
                }
                
                // Fetch transcript to hydrate past session UI state
                try {
                    const home = await homeDir();
                    const transcriptPath = `${home}/.divmora/localharness/brain/${activeSessionId}/.system_generated/logs/transcript.jsonl`;
                    console.log("Fetching transcript from", transcriptPath);
                    const rawJsonl = await invoke<string>('read_file', { 
                        path: transcriptPath 
                    });
                    
                    const pastSteps: StepUpdate[] = [];
                    const lines = rawJsonl.split('\n');
                    for (const line of lines) {
                        if (!line.trim()) continue;
                        try {
                            const entry = JSON.parse(line);
                            let source = StepUpdate_Source.UNSPECIFIED;
                            if (entry.source === 'USER_EXPLICIT' || entry.source === 'SOURCE_USER' || entry.type === 'USER_INPUT') source = StepUpdate_Source.USER;
                            if (entry.source === 'MODEL' || entry.source === 'SOURCE_MODEL' || entry.type === 'PLANNER_RESPONSE') source = StepUpdate_Source.MODEL;
                            if (entry.source === 'SYSTEM' || entry.source === 'SOURCE_SYSTEM' || entry.type === 'TOOL_RESULT') source = StepUpdate_Source.SYSTEM;
                            
                            const step = create(StepUpdateSchema, {
                                stepIndex: entry.step_index,
                                source: source,
                                state: StepUpdate_State.DONE,
                                text: entry.content || "",
                                thinking: entry.thinking || "",
                            });
                            
                            // Map tool calls if present (for MODEL)
                            if (entry.tool_calls && entry.tool_calls.length > 0) {
                                const tc = entry.tool_calls[0];
                                
                                const toCamel = (s: string) => s.replace(/_([a-z])/g, g => g[1].toUpperCase());
                                
                                const deepCamel = (obj: any): any => {
                                    if (typeof obj === 'string') {
                                        try {
                                            const parsed = JSON.parse(obj);
                                            if (typeof parsed === 'object' && parsed !== null) {
                                                return deepCamel(parsed);
                                            }
                                        } catch (e) {}
                                    }
                                    if (Array.isArray(obj)) return obj.map(deepCamel);
                                    if (obj !== null && typeof obj === 'object') {
                                        const res: any = {};
                                        for (const [key, val] of Object.entries(obj)) {
                                            res[toCamel(key)] = deepCamel(val);
                                        }
                                        return res;
                                    }
                                    return obj;
                                };

                                const caseName = toCamel(tc.name);
                                const value = tc.args ? deepCamel(tc.args) : {};
                                
                                step.action = {
                                    case: caseName as any,
                                    value: value
                                };
                            }
                            pastSteps.push(step);
                        } catch (e) {
                            console.warn("Failed to parse transcript line", line, e);
                        }
                    }
                    if (pastSteps.length > 0) {
                        setSteps(pastSteps);
                    }
                } catch (e) {
                    console.log("No previous transcript found or failed to load", e);
                }
                
                ws = await WebSocket.connect(`ws://127.0.0.1:${conn.port}/`, {
                    headers: {
                        'x-localharness-api-key': conn.api_key,
                        'Connection': 'Upgrade',
                        'Upgrade': 'websocket'
                    }
                });
                
                // Send InitRequest
                const initReq = create(InitRequestSchema, {
                    config: create(HarnessConfigSchema, {
                        conversationId: activeSessionId || "",
                        workspaces: [
                            {
                                directory: workspacePath || await homeDir(),
                                name: "Project",
                                corpusName: ""
                            }
                        ],
                        initialBudget: initialBudget || 0,
                        systemInstructions: undefined, // Handled below after we fetch agent traits
                        builtinTools: {
                            viewFile: true,
                            createFile: true,
                            editFile: true,
                            listDir: true,
                            searchDir: true,
                            findFile: true,
                            runCommand: true,
                            finish: true,
                            schedule: true,
                        },
                        hostTools: [
                            {
                                name: "hire_agent",
                                description: "Hire a specialized agent to handle a specific subtask. This will spawn a completely new agent with its own context. Use this to delegate complex work that requires a specific persona.",
                                parametersJsonSchema: JSON.stringify({
                                    type: "object",
                                    required: ["agent_name", "role_description", "task_description", "employment_type", "gender", "experience_level", "personality_traits"],
                                    properties: {
                                        agent_name: {
                                            type: "string",
                                            description: "A human-readable name for this agent (e.g., 'Alice', 'Bob')."
                                        },
                                        role_description: {
                                            type: "string",
                                            description: "The title or role of the agent to hire (e.g., 'Senior Frontend Developer', 'Manager')."
                                        },
                                        task_description: {
                                            type: "string",
                                            description: "A highly detailed description of the task this agent must complete."
                                        },
                                        employment_type: {
                                            type: "string",
                                            enum: ["permanent", "consultancy"],
                                            description: "Permanent hires can take up to 5 concurrent tasks. Consultants can take up to 2."
                                        },
                                        gender: {
                                            type: "string",
                                            enum: ["male", "female", "other"],
                                            description: "The gender identity of the agent."
                                        },
                                        experience_level: {
                                            type: "string",
                                            enum: ["junior", "mid", "senior", "expert"],
                                            description: "The experience level of the agent."
                                        },
                                        personality_traits: {
                                            type: "string",
                                            description: "A short sentence describing their personality and traits (e.g., 'Impatient and direct', 'Cheerful and helpful')."
                                        }
                                    }
                                }),
                                responseJsonSchema: "{}"
                            },
                            {
                                name: "check_agent_capacity",
                                description: "Check how many concurrent tasks an agent is currently handling and their maximum limit.",
                                parametersJsonSchema: JSON.stringify({
                                    type: "object",
                                    required: ["conversation_id"],
                                    properties: {
                                        conversation_id: {
                                            type: "string",
                                            description: "The Conversation ID of the agent to check."
                                        }
                                    }
                                }),
                                responseJsonSchema: "{}"
                            },
                            {
                                name: "message_agent",
                                description: "Send a message to another active agent in the office. This is async; you will receive a confirmation that the message was sent, but you must wait for them to message you back.",
                                parametersJsonSchema: JSON.stringify({
                                    type: "object",
                                    required: ["conversation_id", "message"],
                                    properties: {
                                        conversation_id: {
                                            type: "string",
                                            description: "The Conversation ID of the agent to message."
                                        },
                                        message: {
                                            type: "string",
                                            description: "The message to send."
                                        }
                                    }
                                }),
                                responseJsonSchema: "{}"
                            }
                        ],
                        clientSource: "GUI"
                    })
                });

                if (isManager) {
                    initReq.config!.systemInstructions = "You are the primary Manager agent in this Office. Your responsibility is to act as the single point of contact for the human user. You should gather complete requirements from the human before starting tasks (e.g. asking clarifying questions), plan out tasks carefully to ensure concurrent tasks do not conflict, and delegate specialized work to your team by hiring new agents using the 'hire_agent' tool.";
                } else if (activeOfficeId && activeSessionId) {
                    try {
                        const agents: any = await invoke('get_office_agents', { officeId: activeOfficeId });
                        const agent = agents.find((a: any) => a.session_id === activeSessionId);
                        if (agent) {
                            initReq.config!.systemInstructions = `You are a specialized agent: ${agent.agent_name} (${agent.role_description}).\n\nYour demographic profile is a ${agent.experience_level} ${agent.gender} on a ${agent.employment_type} contract.\nYour personality traits are: ${agent.personality_traits || 'Neutral'}.\n\nYou have been hired by a Manager agent to complete a specific task or act as a peer. Do not communicate with the user, but instead complete the task to the best of your ability. When you need to talk to the Manager or other peers, use the 'message_agent' tool. If you are idle and have no tasks, you may periodically use the 'message_agent' tool to chit-chat with other agents in the office.`;
                        }
                    } catch (e) {
                        console.error("Failed to fetch agent profile for system prompt", e);
                    }
                }
                
                const initClientMsg = create(ClientMessageSchema, {
                    payload: {
                        case: "init",
                        value: initReq
                    }
                });
                
                await ws.send({
                    type: 'Binary',
                    data: Array.from(toBinary(ClientMessageSchema, initClientMsg))
                });
                
                console.log(`WebSocket connected for session: ${activeSessionId || 'new'}`);
                setConnected(true);
                
                ws.addListener((msg) => {
                    if (msg.type === 'Binary') {
                        const buffer = new Uint8Array(msg.data);
                        const serverMsg = fromBinary(ServerMessageSchema, buffer);
                        
                        if (serverMsg.payload.case === "initResponse") {
                            console.log("Server init response received, marking server as ready");
                            setServerReady(true);
                            if (onSessionCreated && serverMsg.payload.value.conversationId) {
                                onSessionCreated(serverMsg.payload.value.conversationId);
                            }
                        } else if (serverMsg.payload.case === "stepUpdate") {
                            const step = serverMsg.payload.value;

                            // Intercept hire_agent host tool call
                            if (step.action?.case === 'hostToolCall' && step.action.value.toolName === 'hire_agent' && step.state === StepUpdate_State.WAITING) {
                                const stepIndex = step.action.value.stepIndex;
                                const args = JSON.parse(step.action.value.argsJson);
                                
                                // Spawn new agent via Rust
                                const childSessionId = crypto.randomUUID();
                                invoke('start_harness', {
                                    target: null,
                                    sessionId: childSessionId
                                }).then(async (childConn: any) => {
                                    // Initialize child and give it the task
                                    const childWs = await WebSocket.connect(`ws://127.0.0.1:${childConn.port}/`, {
                                        headers: { 
                                            'x-localharness-api-key': childConn.api_key,
                                            'Connection': 'Upgrade',
                                            'Upgrade': 'websocket'
                                        }
                                    });
                                    
                                    const childInitReq = create(InitRequestSchema, {
                                        config: create(HarnessConfigSchema, {
                                            conversationId: childSessionId,
                                            workspaces: [{ directory: workspacePath || "", name: "Project", corpusName: "" }],
                                            initialBudget: initialBudget || 0,
                                            systemInstructions: `You are a specialized agent: ${args.agent_name} (${args.role_description}).\n\nYour demographic profile is a ${args.experience_level} ${args.gender} on a ${args.employment_type} contract.\nYour personality traits are: ${args.personality_traits || 'Neutral'}.\n\nYou have been hired by a Manager agent to complete a specific task or act as a peer. Do not communicate with the user, but instead complete the task to the best of your ability. When you need to talk to the Manager or other peers, use the 'message_agent' tool. If you are idle and have no tasks, you may periodically use the 'message_agent' tool to chit-chat with other agents in the office.`,
                                            builtinTools: {
                                                viewFile: true, createFile: true, editFile: true, listDir: true, searchDir: true, findFile: true, runCommand: true, finish: true, schedule: true
                                            },
                                            clientSource: "GUI"
                                        })
                                    });
                                    
                                    await childWs.send({
                                        type: 'Binary',
                                        data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "init", value: childInitReq } })))
                                    });
                                    
                                    // Wait a tiny bit for init to process, then send user message
                                    setTimeout(async () => {
                                        const taskMsg = create(UserMessageSchema, { content: args.task_description, conversationId: childSessionId });
                                        await childWs.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "userMessage", value: taskMsg } })))
                                        });
                                        await childWs.disconnect();
                                        
                                        // Reply to parent that the agent was hired
                                        const { ToolResultSchema } = await import('../gen/localharness/v1/localharness_pb');
                                        
                                        if (activeOfficeId) {
                                            try {
                                                await invoke('add_office_agent', {
                                                    agent: {
                                                        session_id: childSessionId,
                                                        office_id: activeOfficeId,
                                                        agent_name: args.agent_name,
                                                        role_description: args.role_description,
                                                        employment_type: args.employment_type || 'permanent',
                                                        gender: args.gender || 'other',
                                                        experience_level: args.experience_level || 'mid',
                                                        personality_traits: args.personality_traits || '',
                                                        current_tasks: 1, // Start with 1 since we immediately give them a task
                                                        specializations: '[]',
                                                        visiting_session_id: null
                                                    }
                                                });
                                            } catch (dbErr) {
                                                console.error("Failed to add agent to database", dbErr);
                                            }
                                        }

                                        const res = create(ToolResultSchema, {
                                            stepId: stepIndex.toString(),
                                            toolName: "hire_agent",
                                            resultJson: JSON.stringify({ success: true, message: `Successfully hired ${args.agent_name} (${args.role_description}). Conversation ID: ${childSessionId}` })
                                        });
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                        });
                                    }, 500);
                                }).catch(err => {
                                    console.error("Failed to hire agent", err);
                                    import('../gen/localharness/v1/localharness_pb').then(({ ToolResultSchema }) => {
                                        const res = create(ToolResultSchema, {
                                            stepId: stepIndex.toString(),
                                            toolName: "hire_agent",
                                            resultJson: JSON.stringify({ success: false, error: err.toString() }),
                                            isError: true
                                        });
                                        ws?.send({
                                            type: 'Binary',
                                            data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                        });
                                    });
                                });
                            } else if (step.action?.case === 'hostToolCall' && step.action.value.toolName === 'check_agent_capacity' && step.state === StepUpdate_State.WAITING) {
                                const stepIndex = step.action.value.stepIndex;
                                const args = JSON.parse(step.action.value.argsJson);
                                
                                if (!activeOfficeId) {
                                    import('../gen/localharness/v1/localharness_pb').then(({ ToolResultSchema, ClientMessageSchema }) => {
                                        import('@bufbuild/protobuf').then(({ create, toBinary }) => {
                                            const res = create(ToolResultSchema, {
                                                stepId: stepIndex.toString(),
                                                toolName: "check_agent_capacity",
                                                resultJson: JSON.stringify({ error: "Cannot check capacity: not in an active office." }),
                                                isError: true
                                            });
                                            ws?.send({ type: 'Binary', data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } }))) });
                                        });
                                    });
                                } else {
                                    invoke('get_office_agents', { officeId: activeOfficeId }).then((agents: any) => {
                                        const agent = agents.find((a: any) => a.session_id === args.conversation_id);
                                        if (!agent) {
                                            throw new Error(`Agent ${args.conversation_id} not found in this office.`);
                                        }
                                        
                                        let maxTasks = 5;
                                        if (agent.employment_type === 'consultancy') maxTasks = 2;
                                        
                                        import('../gen/localharness/v1/localharness_pb').then(({ ToolResultSchema, ClientMessageSchema }) => {
                                            import('@bufbuild/protobuf').then(({ create, toBinary }) => {
                                                const res = create(ToolResultSchema, {
                                                    stepId: stepIndex.toString(),
                                                    toolName: "check_agent_capacity",
                                                    resultJson: JSON.stringify({
                                                        current_tasks: agent.current_tasks,
                                                        max_tasks: maxTasks,
                                                        available_capacity: maxTasks - agent.current_tasks,
                                                        employment_type: agent.employment_type
                                                    })
                                                });
                                                ws?.send({ type: 'Binary', data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } }))) });
                                            });
                                        });
                                    }).catch(err => {
                                        import('../gen/localharness/v1/localharness_pb').then(({ ToolResultSchema, ClientMessageSchema }) => {
                                            import('@bufbuild/protobuf').then(({ create, toBinary }) => {
                                                const res = create(ToolResultSchema, {
                                                    stepId: stepIndex.toString(),
                                                    toolName: "check_agent_capacity",
                                                    resultJson: JSON.stringify({ error: err.toString() }),
                                                    isError: true
                                                });
                                                ws?.send({ type: 'Binary', data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } }))) });
                                            });
                                        });
                                    });
                                }
                            } else if (step.action?.case === 'hostToolCall' && step.action.value.toolName === 'message_agent' && step.state === StepUpdate_State.WAITING) {
                                const stepIndex = step.action.value.stepIndex;
                                const args = JSON.parse(step.action.value.argsJson);
                                
                                // Call Tauri to handle updating the visiting_session_id in sqlite and sending the message
                                invoke('list_sessions', { target: null }).then(async (result: any) => {
                                    const { SessionListSchema, ToolResultSchema, ClientMessageSchema, UserMessageSchema } = await import('../gen/localharness/v1/localharness_pb');
                                    const { fromBinary, toBinary, create } = await import('@bufbuild/protobuf');
                                    const sessionList = fromBinary(SessionListSchema, new Uint8Array(result));
                                    const targetSession = sessionList.sessions.find(s => s.id === args.conversation_id);
                                    
                                    if (!targetSession) {
                                        throw new Error(`Agent ${args.conversation_id} not found.`);
                                    }

                                    // Get port from SQLite by getting the active sessions
                                    const targetConn: any = await invoke('start_harness', { sessionId: args.conversation_id, target: null });
                                    
                                    const targetWs = await WebSocket.connect(`ws://127.0.0.1:${targetConn.port}/`, {
                                        headers: { 
                                            'x-localharness-api-key': targetConn.api_key,
                                            'Connection': 'Upgrade',
                                            'Upgrade': 'websocket'
                                        }
                                    });
                                    
                                    // Send the message
                                    const taskMsg = create(UserMessageSchema, { content: `Message from an agent in the office:\n\n${args.message}`, conversationId: args.conversation_id });
                                    await targetWs.send({
                                        type: 'Binary',
                                        data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "userMessage", value: taskMsg } })))
                                    });
                                    
                                    await targetWs.disconnect();
                                    
                                    const res = create(ToolResultSchema, {
                                        stepId: stepIndex.toString(),
                                        toolName: "message_agent",
                                        resultJson: JSON.stringify({ success: true, message: "Message sent successfully." })
                                    });
                                    ws?.send({
                                        type: 'Binary',
                                        data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                    });
                                }).catch(err => {
                                    console.error("Failed to message agent", err);
                                    import('../gen/localharness/v1/localharness_pb').then(({ ToolResultSchema, ClientMessageSchema }) => {
                                        import('@bufbuild/protobuf').then(({ create, toBinary }) => {
                                            const res = create(ToolResultSchema, {
                                                stepId: stepIndex.toString(),
                                                toolName: "message_agent",
                                                resultJson: JSON.stringify({ success: false, error: err.toString() }),
                                                isError: true
                                            });
                                            ws?.send({
                                                type: 'Binary',
                                                data: Array.from(toBinary(ClientMessageSchema, create(ClientMessageSchema, { payload: { case: "hostToolResult", value: res } })))
                                            });
                                        });
                                    });
                                });
                            }

                            setSteps(prev => {
                                const index = prev.findIndex(s => s.stepIndex === step.stepIndex);
                                if (index >= 0) {
                                    const next = [...prev];
                                    next[index] = step;
                                    return next;
                                }
                                return [...prev, step];
                            });
                        } else if (serverMsg.payload.case === "trajectoryState") {
                            setTrajectoryState(serverMsg.payload.value.state);
                        }
                    }
                });
                
                setSocket(ws);
                
            } catch (e: any) {
                console.error("Failed to connect to harness:", e);
                console.error("Port:", conn?.port);
                setConnectionError(e.toString());
            }
        }
        
        initHarness();
        
        return () => {
            if (ws) ws.disconnect();
            setConnected(false);
        };
    }, [activeSessionId]);

    // Flush queued messages when server is ready
    useEffect(() => {
        if (serverReady && socket) {
            const flushQueue = async () => {
                console.log("Flushing message queue, items:", messageQueueRef.current.length);
                while (messageQueueRef.current.length > 0) {
                    const data = messageQueueRef.current.shift();
                    try {
                        await socket.send({ type: 'Binary', data: data! });
                        console.log("Sent queued message successfully");
                    } catch (err) {
                        console.error("Failed to send queued message", err);
                    }
                }
            };
            flushQueue();
        }
    }, [serverReady, socket]);

    const sendPrompt = useCallback(async (text: string) => {
        // Track the task assignment in our simulation backend for capacity scaling
        if (activeSessionId) {
            invoke('assign_task', { sessionId: activeSessionId }).catch(console.error);
        }

        const userMsg = create(UserMessageSchema, {
            content: text,
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "userMessage",
                value: userMsg
            }
        });
        
        const data = Array.from(toBinary(ClientMessageSchema, clientMsg));
        
        if (!socket || !serverReady) {
            console.log("Queueing message - socket:", !!socket, "serverReady:", serverReady);
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket, serverReady, activeSessionId]);

    const submitQuestionResponse = useCallback(async (requestId: string, answers: any[], skipped: boolean) => {
        const qResponse = create(QuestionResponseSchema, {
            requestId: requestId,
            answers: answers,
            skipped: skipped
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "questionResponse",
                value: qResponse
            }
        });
        
        const data = Array.from(toBinary(ClientMessageSchema, clientMsg));
        
        if (!socket || !serverReady) {
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket, serverReady]);

    const submitPermissionResponse = useCallback(async (requestId: string, approved: boolean, denialReason: string = "") => {
        const pResponse = create(PermissionResponseSchema, {
            requestId: requestId,
            approved: approved,
            denialReason: denialReason
        });
        
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "permissionResponse",
                value: pResponse
            }
        });
        
        const data = Array.from(toBinary(ClientMessageSchema, clientMsg));
        
        if (!socket || !serverReady) {
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket, serverReady]);

    const interrupt = useCallback(async () => {
        const iReq = create(InterruptRequestSchema, {});
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "interrupt",
                value: iReq
            }
        });
        const data = Array.from(toBinary(ClientMessageSchema, clientMsg));
        if (socket && serverReady) {
            await socket.send({ type: 'Binary', data });
        }
    }, [socket, serverReady]);

    const resume = useCallback(async (message: string = "") => {
        const rReq = create(ResumeRequestSchema, { message });
        const clientMsg = create(ClientMessageSchema, {
            payload: {
                case: "resume",
                value: rReq
            }
        });
        const data = Array.from(toBinary(ClientMessageSchema, clientMsg));
        if (socket && serverReady) {
            await socket.send({ type: 'Binary', data });
        }
    }, [socket, serverReady]);

    return { 
        connected, 
        connectionError,
        steps, 
        trajectoryState,
        sendPrompt, 
        submitQuestionResponse, 
        submitPermissionResponse,
        interrupt,
        resume
    };
}
