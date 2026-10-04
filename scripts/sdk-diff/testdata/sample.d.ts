// Hand-written sample in the shape of the SDK typings (not copied from the SDK).
declare type StdoutMessage = core.Msg | ControlResponse | ControlRequest | KeepAlive;
export declare type Msg = Assistant | Result | Sys | NewThing;
declare type Assistant = {
    type: 'assistant';
    message: {
        type: 'not_a_top_level_type';
    };
};
declare type ResultOk = {
    type: 'result';
    subtype: 'success';
};
declare type ResultErr = {
    /** comment: type: 'nope' */
    type: 'result';
    subtype:
        | 'error_during_execution'
        | 'error_max_turns';
};
export declare type Result = ResultOk | ResultErr;
declare type Sys = SysInit | SysBrandNew;
declare type SysInit = {
    type: 'system';
    subtype: 'init';
};
declare type SysBrandNew = {
    type: 'system';
    subtype: 'brand_new_subtype';
};
declare type NewThing = {
    type: 'brand_new_type';
};
declare type ControlResponse = {
    type: 'control_response';
};
declare type ControlRequest = {
    type: 'control_request';
    request: Inner;
};
declare type KeepAlive = {
    type: 'keep_alive';
};
declare type Inner = Interrupt | CanUse | Unknown<Thing>;
declare type Interrupt = {
    subtype: 'interrupt';
};
declare type CanUse = {
    subtype: 'can_use_tool';
};
declare type SDKControlRequestInner = Interrupt | CanUse | BrandNewRequest;
declare type BrandNewRequest = {
    subtype: 'brand_new_request';
};
