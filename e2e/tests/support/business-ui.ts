// 真实业务场景界面动作的统一出口：按领域拆成模板（business-templates）、
// 台账（business-records）与交付（business-delivery）三个文件，spec 仍从
// 这里导入，保持业务编排代码简洁。
export * from "./business-templates";
export * from "./business-records";
export * from "./business-delivery";
