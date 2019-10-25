package org.aion.avm.core.miscvisitors;

import org.aion.avm.core.ClassToolchain;
import org.aion.avm.core.types.GeneratedClassConsumer;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.FieldVisitor;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.tree.FieldNode;
import org.objectweb.asm.tree.MethodNode;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import static org.objectweb.asm.Opcodes.ACC_INTERFACE;
import static org.objectweb.asm.Opcodes.ACC_PRIVATE;
import static org.objectweb.asm.Opcodes.ALOAD;
import static org.objectweb.asm.Opcodes.INVOKESPECIAL;
import static org.objectweb.asm.Opcodes.RETURN;
import static org.objectweb.asm.Opcodes.V1_6;

/**
 * A visitor which generates a class containing all the declared fields and clinit method of an interface. (issue-208)
 * Name of the generated class is produced by concatenating the interface name and $FIELDS suffix.
 * If such class name has already been defined by the user, next available name is generated by adding a number to the suffix.
 */
public class InterfaceFieldClassGeneratorVisitor extends ClassToolchain.ToolChainClassVisitor {

    private GeneratedClassConsumer consumer;
    private Map<String, String> interfaceFieldClassNames;
    private String javaLangObject;

    private boolean isInterface = false;
    private String className = null;
    private int access = 0;
    private List<FieldNode> fields = new ArrayList<>();
    private MethodNode clinit = null;
    private List<String> innerClassNames;

    private String generatedClassName;
    private String prefix;

    /**
     * Create an InterfaceFieldClassGeneratorVisitor instance.
     *
     * @param consumer                 A container to collect all the generated classes
     * @param interfaceFieldClassNames HashMap containing the mapping between class name and generated FIELDS class
     * @param javaLangObjectSlashName  The java/lang/Object class className, either pre-rename or post-rename
     */
    public InterfaceFieldClassGeneratorVisitor(GeneratedClassConsumer consumer, Map<String, String> interfaceFieldClassNames, String javaLangObjectSlashName) {
        super(Opcodes.ASM6);
        this.consumer = consumer;
        this.interfaceFieldClassNames = interfaceFieldClassNames;
        this.javaLangObject = javaLangObjectSlashName;
        this.innerClassNames = new ArrayList<>();
    }

    @Override
    public void visit(int version, int access, String name, String signature, String superName, String[] interfaces) {
        if ((access & ACC_INTERFACE) != 0) {
            this.isInterface = true;
            this.className = name;
            this.access = access;
            this.prefix = className + "$FIELDS";
        }
        super.visit(version, access, name, signature, superName, interfaces);
    }

    @Override
    public MethodVisitor visitMethod(int access, String name, String descriptor, String signature, String[] exceptions) {
        MethodVisitor mv;
        if (this.isInterface && "<clinit>".equals(name)) {
            // If this is a clinit, capture it into the MethodNode, to write it to the generated class at the end.
            // Clinit will be removed from the interface in InterfaceFieldNameMappingVisitor
            this.clinit = new MethodNode(access, name, descriptor, signature, exceptions);
            mv = new MethodVisitor(Opcodes.ASM6, this.clinit) {
                // update the clinit field owners to the generated class name.
                public void visitFieldInsn(int opcode, String owner, String name, String descriptor) {
                    if (className.equals(owner)) {
                        if (generatedClassName == null) {
                            generatedClassName = getNextAvailableFieldsClassName();
                        }
                        owner = generatedClassName;
                    }
                    super.visitFieldInsn(opcode, owner, name, descriptor);
                }
            };
        } else {
            mv = super.visitMethod(access, name, descriptor, signature, exceptions);
        }
        return mv;
    }

    @Override
    public FieldVisitor visitField(int access, String name, String descriptor, String signature, Object value) {
        FieldVisitor fv;
        if (isInterface) {
            // store the fields to write them to the generated class at the end. Fields will be removed from the interface in InterfaceFieldNameMappingVisitor.
            FieldNode field = new FieldNode(access, name, descriptor, signature, value);
            fields.add(field);
            fv = null;
        } else {
            fv = super.visitField(access, name, descriptor, signature, value);
        }
        return fv;
    }

    @Override
    public void visitInnerClass(String name, String outer, String innerName, int access) {
        if (isInterface && name.startsWith(prefix)) {
            innerClassNames.add(name);
        }
        super.visitInnerClass(name, outer, innerName, access);
    }

    @Override
    public void visitEnd() {
        // generate the class only if interface has declared fields
        if (isInterface && fields.size() > 0) {
            /* AKI-329: Previously generated classes using InterfaceFieldMappingVisitor had the FIELDS suffix. To deserialize classes correctly and in
             the same order, this suffix is kept for re-transformation. However, this name can collide with any interface inner class called FIELDS.
             So all the inner class names starting with FIELDS are collected so that the generated class name will not collide with any preexisting user classes.
            */
            if (generatedClassName == null) {
                generatedClassName = getNextAvailableFieldsClassName();
            }

            interfaceFieldClassNames.put(className, generatedClassName);
            String genSuperName = javaLangObject;
            int genAccess = access & ~ACC_INTERFACE;

            ClassWriter cw = new ClassWriter(0);

            // class declaration
            cw.visit(V1_6, genAccess, generatedClassName, null, genSuperName, null);

            // default constructor
            {
                MethodVisitor mv = cw.visitMethod(ACC_PRIVATE, "<init>", "()V", null, null);
                mv.visitCode();
                mv.visitVarInsn(ALOAD, 0); //load the first local variable: this
                mv.visitMethodInsn(INVOKESPECIAL, javaLangObject, "<init>", "()V");
                mv.visitInsn(RETURN);
                mv.visitMaxs(1, 1);
                mv.visitEnd();
            }

            // fields
            for (FieldNode field : fields) {
                field.accept(cw);
            }

            // clinit
            if (clinit != null) {
                clinit.accept(cw);
            }

            consumer.accept(genSuperName, generatedClassName, cw.toByteArray());
        }
    }

    // This method tries to find the next the available suffix to assign to the generated FIELDS class.
    // Looping over the classes is acceptable since there can only be small number of them in user code.
    private String getNextAvailableFieldsClassName() {
        int suffix = 0;
        if (!innerClassNames.contains(prefix)) {
            return prefix;
        } else {
            while (innerClassNames.contains(prefix + suffix)) {
                suffix++;
            }
            return prefix + suffix;
        }
    }
}
