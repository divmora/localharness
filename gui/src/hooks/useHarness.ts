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

export function useHarness(activeSessionId: string | null, workspacePath?: string | null, initialBudget?: number) {
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
            return;
        }

        async function initHarness() {
            setConnectionError(null);
            setSteps([]);
            setServerReady(false);
            
            let conn: HarnessConnection | null = null;
            
            try {
                console.log("Requesting sidecar from Rust...");
                try {
                    conn = await invoke<HarnessConnection>('start_harness', { target: null });
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
                        'x-localharness-api-key': conn.api_key
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
                        clientSource: "GUI"
                    })
                });
                
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
                        } else if (serverMsg.payload.case === "stepUpdate") {
                            const step = serverMsg.payload.value;
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
    }, [socket, serverReady]);

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
