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
    PermissionResponseSchema
} from '../gen/localharness/v1/localharness_pb';

export interface ConnectionTarget {
    kind: "local" | "ssh";
    host?: string;
    user?: string;
    port?: number;
    key_path?: string;
}

interface HarnessConnection {
    port: number;
    api_key: string;
}

export function useHarness(activeSessionId: string | null, connectionTarget: ConnectionTarget | null = null, workspacePath?: string | null) {
    const [connected, setConnected] = useState(false);
    const [connectionError, setConnectionError] = useState<string | null>(null);
    const [steps, setSteps] = useState<StepUpdate[]>([]);
    const [socket, setSocket] = useState<WebSocket | null>(null);
    const messageQueueRef = useRef<any[]>([]);

    useEffect(() => {
        let ws: WebSocket | null = null;
        
        if (!activeSessionId) {
            setConnected(false);
            setConnectionError(null);
            setSteps([]);
            setSocket(null);
            return;
        }

        async function initHarness() {
            setConnectionError(null);
            setSteps([]);
            
            try {
                console.log("Requesting sidecar from Rust with target:", connectionTarget);
                let conn: HarnessConnection;
                try {
                    conn = await invoke<HarnessConnection>('start_harness', { target: connectionTarget });
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
                    const rawJsonl = await invoke<string>('read_target_file', { 
                        target: connectionTarget, 
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
                
                ws = await WebSocket.connect(`ws://localhost:${conn.port}/`, {
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
                        
                        if (serverMsg.payload.case === "stepUpdate") {
                            const step = serverMsg.payload.value;
                            setSteps(prev => [...prev, step]);
                        }
                    }
                });
                
                setSocket(ws);
                
                // Flush queued messages
                while (messageQueueRef.current.length > 0) {
                    const data = messageQueueRef.current.shift();
                    try {
                        await ws.send({ type: 'Binary', data });
                    } catch (err) {
                        console.error("Failed to send queued message", err);
                    }
                }
                
            } catch (e: any) {
                console.error("Failed to connect to harness:", e);
                setConnectionError(e.toString());
            }
        }
        
        initHarness();
        
        return () => {
            if (ws) ws.disconnect();
            setConnected(false);
        };
    }, [activeSessionId]);

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
        
        if (!socket) {
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket]);

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
        
        if (!socket) {
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket]);

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
        
        if (!socket) {
            messageQueueRef.current.push(data);
            return;
        }
        
        await socket.send({ type: 'Binary', data });
    }, [socket]);

    return { 
        connected, 
        connectionError,
        steps, 
        sendPrompt, 
        submitQuestionResponse, 
        submitPermissionResponse 
    };
}
