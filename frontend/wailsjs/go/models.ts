export namespace bridge {
	
	export class Document {
	    id: string;
	    name: string;
	    width: number;
	    height: number;
	    resolution: number;
	    dirty: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Document(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.resolution = source["resolution"];
	        this.dirty = source["dirty"];
	    }
	}
	export class Snapshot {
	    tabs: Document[];
	    activeId: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tabs = this.convertValues(source["tabs"], Document);
	        this.activeId = source["activeId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WindowState {
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class pendingImportFile {
	
	
	    static createFrom(source: any = {}) {
	        return new pendingImportFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace domain {
	
	export class ColorBalanceSettings {
	    shadowCyanRed: number;
	    shadowMagentaGreen: number;
	    shadowYellowBlue: number;
	    midCyanRed: number;
	    midMagentaGreen: number;
	    midYellowBlue: number;
	    highlightCyanRed: number;
	    highlightMagentaGreen: number;
	    highlightYellowBlue: number;
	    preserveLuminosity: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ColorBalanceSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shadowCyanRed = source["shadowCyanRed"];
	        this.shadowMagentaGreen = source["shadowMagentaGreen"];
	        this.shadowYellowBlue = source["shadowYellowBlue"];
	        this.midCyanRed = source["midCyanRed"];
	        this.midMagentaGreen = source["midMagentaGreen"];
	        this.midYellowBlue = source["midYellowBlue"];
	        this.highlightCyanRed = source["highlightCyanRed"];
	        this.highlightMagentaGreen = source["highlightMagentaGreen"];
	        this.highlightYellowBlue = source["highlightYellowBlue"];
	        this.preserveLuminosity = source["preserveLuminosity"];
	    }
	}
	export class BlackWhiteSettings {
	    reds: number;
	    yellows: number;
	    greens: number;
	    cyans: number;
	    blues: number;
	    magentas: number;
	    tint: boolean;
	    tintHue: number;
	    tintSaturation: number;
	
	    static createFrom(source: any = {}) {
	        return new BlackWhiteSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reds = source["reds"];
	        this.yellows = source["yellows"];
	        this.greens = source["greens"];
	        this.cyans = source["cyans"];
	        this.blues = source["blues"];
	        this.magentas = source["magentas"];
	        this.tint = source["tint"];
	        this.tintHue = source["tintHue"];
	        this.tintSaturation = source["tintSaturation"];
	    }
	}
	export class GrainSettings {
	    amount: number;
	    size: number;
	    roughness: number;
	    seed: number;
	
	    static createFrom(source: any = {}) {
	        return new GrainSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.amount = source["amount"];
	        this.size = source["size"];
	        this.roughness = source["roughness"];
	        this.seed = source["seed"];
	    }
	}
	export class RGB {
	    red: number;
	    green: number;
	    blue: number;
	
	    static createFrom(source: any = {}) {
	        return new RGB(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	    }
	}
	export class GradientMapSettings {
	    shadows: RGB;
	    highlights: RGB;
	    reversed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GradientMapSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shadows = this.convertValues(source["shadows"], RGB);
	        this.highlights = this.convertValues(source["highlights"], RGB);
	        this.reversed = source["reversed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExposureSettings {
	    exposure: number;
	    offset: number;
	    gamma: number;
	
	    static createFrom(source: any = {}) {
	        return new ExposureSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exposure = source["exposure"];
	        this.offset = source["offset"];
	        this.gamma = source["gamma"];
	    }
	}
	export class CurvePoint {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new CurvePoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class CurvesSettings {
	    channel: string;
	    channels: CurvePoint[][];
	
	    static createFrom(source: any = {}) {
	        return new CurvesSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel = source["channel"];
	        this.channels = this.convertValues(source["channels"], CurvePoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LevelRange {
	    black: number;
	    gamma: number;
	    white: number;
	    outputBlack: number;
	    outputWhite: number;
	
	    static createFrom(source: any = {}) {
	        return new LevelRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.black = source["black"];
	        this.gamma = source["gamma"];
	        this.white = source["white"];
	        this.outputBlack = source["outputBlack"];
	        this.outputWhite = source["outputWhite"];
	    }
	}
	export class LevelsSettings {
	    channel: string;
	    ranges: LevelRange[];
	
	    static createFrom(source: any = {}) {
	        return new LevelsSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel = source["channel"];
	        this.ranges = this.convertValues(source["ranges"], LevelRange);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Adjustment {
	    kind: string;
	    hue: number;
	    saturation: number;
	    lightness: number;
	    colorize: boolean;
	    hsvSettings?: number[];
	    levels: LevelsSettings;
	    curves: CurvesSettings;
	    exposureSettings?: ExposureSettings;
	    gradientMapSettings?: GradientMapSettings;
	    grainSettings?: GrainSettings;
	    blackWhiteSettings?: BlackWhiteSettings;
	    colorBalanceSettings?: ColorBalanceSettings;
	    blurRadius?: number;
	    motionAngle?: number;
	    motionDistance?: number;
	    noiseAmount?: number;
	    noiseGaussian?: boolean;
	    noiseMonochromatic?: boolean;
	    noiseSeed?: number;
	
	    static createFrom(source: any = {}) {
	        return new Adjustment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.hue = source["hue"];
	        this.saturation = source["saturation"];
	        this.lightness = source["lightness"];
	        this.colorize = source["colorize"];
	        this.hsvSettings = source["hsvSettings"];
	        this.levels = this.convertValues(source["levels"], LevelsSettings);
	        this.curves = this.convertValues(source["curves"], CurvesSettings);
	        this.exposureSettings = this.convertValues(source["exposureSettings"], ExposureSettings);
	        this.gradientMapSettings = this.convertValues(source["gradientMapSettings"], GradientMapSettings);
	        this.grainSettings = this.convertValues(source["grainSettings"], GrainSettings);
	        this.blackWhiteSettings = this.convertValues(source["blackWhiteSettings"], BlackWhiteSettings);
	        this.colorBalanceSettings = this.convertValues(source["colorBalanceSettings"], ColorBalanceSettings);
	        this.blurRadius = source["blurRadius"];
	        this.motionAngle = source["motionAngle"];
	        this.motionDistance = source["motionDistance"];
	        this.noiseAmount = source["noiseAmount"];
	        this.noiseGaussian = source["noiseGaussian"];
	        this.noiseMonochromatic = source["noiseMonochromatic"];
	        this.noiseSeed = source["noiseSeed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	export class Guide {
	    id: string;
	    axis: string;
	    position: number;
	
	    static createFrom(source: any = {}) {
	        return new Guide(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.axis = source["axis"];
	        this.position = source["position"];
	    }
	}
	export class TextFontRun {
	    location: number;
	    length: number;
	    fontName: string;
	
	    static createFrom(source: any = {}) {
	        return new TextFontRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.location = source["location"];
	        this.length = source["length"];
	        this.fontName = source["fontName"];
	    }
	}
	export class TextColorRun {
	    location: number;
	    length: number;
	    red: number;
	    green: number;
	    blue: number;
	
	    static createFrom(source: any = {}) {
	        return new TextColorRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.location = source["location"];
	        this.length = source["length"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	    }
	}
	export class TextStyle {
	    content: string;
	    fontName: string;
	    fontSize: number;
	    red: number;
	    green: number;
	    blue: number;
	    alignment: string;
	    tracking: number;
	    leading: number;
	    boxSize?: number[];
	    colorRuns?: TextColorRun[];
	    fontRuns?: TextFontRun[];
	
	    static createFrom(source: any = {}) {
	        return new TextStyle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.fontName = source["fontName"];
	        this.fontSize = source["fontSize"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.alignment = source["alignment"];
	        this.tracking = source["tracking"];
	        this.leading = source["leading"];
	        this.boxSize = source["boxSize"];
	        this.colorRuns = this.convertValues(source["colorRuns"], TextColorRun);
	        this.fontRuns = this.convertValues(source["fontRuns"], TextFontRun);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GlowEffect {
	    enabled?: boolean;
	    size: number;
	    red: number;
	    green: number;
	    blue: number;
	    opacity: number;
	
	    static createFrom(source: any = {}) {
	        return new GlowEffect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.size = source["size"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.opacity = source["opacity"];
	    }
	}
	export class OverlayEffect {
	    enabled?: boolean;
	    red: number;
	    green: number;
	    blue: number;
	    opacity: number;
	
	    static createFrom(source: any = {}) {
	        return new OverlayEffect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.opacity = source["opacity"];
	    }
	}
	export class ShadowEffect {
	    enabled?: boolean;
	    angle: number;
	    distance: number;
	    blur: number;
	    red: number;
	    green: number;
	    blue: number;
	    opacity: number;
	
	    static createFrom(source: any = {}) {
	        return new ShadowEffect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.angle = source["angle"];
	        this.distance = source["distance"];
	        this.blur = source["blur"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.opacity = source["opacity"];
	    }
	}
	export class StrokeEffect {
	    enabled?: boolean;
	    size: number;
	    red: number;
	    green: number;
	    blue: number;
	    opacity: number;
	    inside: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StrokeEffect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.size = source["size"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.opacity = source["opacity"];
	        this.inside = source["inside"];
	    }
	}
	export class Effects {
	    stroke?: StrokeEffect;
	    shadow?: ShadowEffect;
	    colorOverlay?: OverlayEffect;
	    innerShadow?: ShadowEffect;
	    outerGlow?: GlowEffect;
	    innerGlow?: GlowEffect;
	
	    static createFrom(source: any = {}) {
	        return new Effects(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stroke = this.convertValues(source["stroke"], StrokeEffect);
	        this.shadow = this.convertValues(source["shadow"], ShadowEffect);
	        this.colorOverlay = this.convertValues(source["colorOverlay"], OverlayEffect);
	        this.innerShadow = this.convertValues(source["innerShadow"], ShadowEffect);
	        this.outerGlow = this.convertValues(source["outerGlow"], GlowEffect);
	        this.innerGlow = this.convertValues(source["innerGlow"], GlowEffect);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ShapeStyle {
	    kind: string;
	    red: number;
	    green: number;
	    blue: number;
	    cornerRadius: number;
	    lineWidth?: number;
	    start?: number[];
	    end?: number[];
	
	    static createFrom(source: any = {}) {
	        return new ShapeStyle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.red = source["red"];
	        this.green = source["green"];
	        this.blue = source["blue"];
	        this.cornerRadius = source["cornerRadius"];
	        this.lineWidth = source["lineWidth"];
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}
	export class Transform {
	    origin: number[];
	    size: number[];
	    rotation: number;
	    flipX: boolean;
	    flipY: boolean;
	    sampling: string;
	
	    static createFrom(source: any = {}) {
	        return new Transform(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.origin = source["origin"];
	        this.size = source["size"];
	        this.rotation = source["rotation"];
	        this.flipX = source["flipX"];
	        this.flipY = source["flipY"];
	        this.sampling = source["sampling"];
	    }
	}
	export class Layer {
	    id: string;
	    name: string;
	    isVisible: boolean;
	    origin: number[];
	    size: number[];
	    rotation: number;
	    flipX: boolean;
	    flipY: boolean;
	    sampling: string;
	    imageFile?: string;
	    parentID?: string;
	    isGroup?: boolean;
	    opacity?: number;
	    blendMode?: string;
	    maskFile?: string;
	    maskEnabled?: boolean;
	    maskSourceID?: string;
	    adjustment?: Adjustment;
	    maskPlacement?: Transform;
	    maskLinked?: boolean;
	    shape?: ShapeStyle;
	    effects?: Effects;
	    text?: TextStyle;
	
	    static createFrom(source: any = {}) {
	        return new Layer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isVisible = source["isVisible"];
	        this.origin = source["origin"];
	        this.size = source["size"];
	        this.rotation = source["rotation"];
	        this.flipX = source["flipX"];
	        this.flipY = source["flipY"];
	        this.sampling = source["sampling"];
	        this.imageFile = source["imageFile"];
	        this.parentID = source["parentID"];
	        this.isGroup = source["isGroup"];
	        this.opacity = source["opacity"];
	        this.blendMode = source["blendMode"];
	        this.maskFile = source["maskFile"];
	        this.maskEnabled = source["maskEnabled"];
	        this.maskSourceID = source["maskSourceID"];
	        this.adjustment = this.convertValues(source["adjustment"], Adjustment);
	        this.maskPlacement = this.convertValues(source["maskPlacement"], Transform);
	        this.maskLinked = source["maskLinked"];
	        this.shape = this.convertValues(source["shape"], ShapeStyle);
	        this.effects = this.convertValues(source["effects"], Effects);
	        this.text = this.convertValues(source["text"], TextStyle);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Document {
	    format: string;
	    version: number;
	    colorSpace: string;
	    documentID: string;
	    width: number;
	    height: number;
	    activeLayerID?: string;
	    resolution?: number;
	    layers: Layer[];
	    guides?: Guide[];
	
	    static createFrom(source: any = {}) {
	        return new Document(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.version = source["version"];
	        this.colorSpace = source["colorSpace"];
	        this.documentID = source["documentID"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.activeLayerID = source["activeLayerID"];
	        this.resolution = source["resolution"];
	        this.layers = this.convertValues(source["layers"], Layer);
	        this.guides = this.convertValues(source["guides"], Guide);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	

}

export namespace render {
	
	export class Bitmap {
	    W: number;
	    H: number;
	    Pix: number[];
	
	    static createFrom(source: any = {}) {
	        return new Bitmap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.W = source["W"];
	        this.H = source["H"];
	        this.Pix = source["Pix"];
	    }
	}

}

